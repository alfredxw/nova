package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"unicode/utf8"

	agent "github.com/alfredxw/denova/agent"
	"github.com/invopop/jsonschema"
)

const readToolContractVersion = 3

// ReadResult is the provider-neutral result returned by every ReadAdapter.
// Offset is one-based when Content should be rendered with source line numbers;
// leave it zero for already-structured content such as directories or JSON.
type ReadResult struct {
	Path           string
	Kind           string
	Content        string
	Offset         int
	ByteOffset     int
	Limit          int
	Total          int
	Truncated      bool
	NextOffset     int
	NextByteOffset int
	Unit           string
}

// ReadMatcher decides whether an Adapter owns one resource path. A matcher
// must not claim paths from another URI scheme.
type ReadMatcher func(context.Context, string) (bool, error)

// ReadAdapter is the seam behind the single model-visible read tool. Identity
// must change whenever routing or read semantics change. Each Adapter
// contributes its exact argument schema and validates the same arguments again
// after the router selects it.
type ReadAdapter interface {
	Identity() agent.CapabilityIdentity
	Name() string
	Parameters() *jsonschema.Schema
	Match(context.Context, string) (bool, error)
	Read(context.Context, string) (ReadResult, error)
}

type typedReadAdapter[T any] struct {
	identity   agent.CapabilityIdentity
	name       string
	parameters *jsonschema.Schema
	match      ReadMatcher
	invoke     func(context.Context, T) (ReadResult, error)
}

// NewReadAdapter constructs a typed Adapter with a provider-visible schema and
// strict runtime decoding. Products use this to add URI-backed resources
// without changing the read tool or weakening another Adapter's parameters.
func NewReadAdapter[T any](identity agent.CapabilityIdentity, name string, match ReadMatcher, invoke func(context.Context, T) (ReadResult, error)) (ReadAdapter, error) {
	if err := validateAdapterIdentity("read Adapter", identity); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("read adapter name is required")
	}
	if match == nil {
		return nil, fmt.Errorf("read adapter %q matcher is required", name)
	}
	if invoke == nil {
		return nil, fmt.Errorf("read adapter %q implementation is required", name)
	}
	parameters, err := agent.GoStruct2ParamsOneOf[T]()
	if err != nil {
		return nil, fmt.Errorf("build read adapter %q schema: %w", name, err)
	}
	schema, err := parameters.ToJSONSchema()
	if err != nil {
		return nil, fmt.Errorf("materialize read adapter %q schema: %w", name, err)
	}
	return &typedReadAdapter[T]{
		identity: identity,
		name:     name, parameters: schema, match: match, invoke: invoke,
	}, nil
}

func (adapter *typedReadAdapter[T]) Identity() agent.CapabilityIdentity {
	if adapter == nil {
		return agent.CapabilityIdentity{}
	}
	return adapter.identity
}

func (adapter *typedReadAdapter[T]) Name() string { return adapter.name }

func (adapter *typedReadAdapter[T]) Parameters() *jsonschema.Schema {
	if adapter == nil || adapter.parameters == nil {
		return nil
	}
	parameters, _ := agent.NewParamsOneOfByJSONSchema(adapter.parameters).ToJSONSchema()
	return parameters
}

func (adapter *typedReadAdapter[T]) Match(ctx context.Context, resourcePath string) (bool, error) {
	if adapter == nil || adapter.match == nil {
		return false, errors.New("read adapter is not configured")
	}
	return adapter.match(ctx, resourcePath)
}

func (adapter *typedReadAdapter[T]) Read(ctx context.Context, arguments string) (ReadResult, error) {
	if adapter == nil || adapter.invoke == nil {
		return ReadResult{}, errors.New("read adapter is not configured")
	}
	input, err := normalizeAndDecode[T](arguments)
	if err != nil {
		return ReadResult{}, fmt.Errorf("decode %s read arguments: %w", adapter.name, err)
	}
	return adapter.invoke(ctx, input)
}

type readTool struct {
	adapters       []ReadAdapter
	schema         *jsonschema.Schema
	desc           string
	maxResultBytes int
}

// Read constructs the single resource-reading tool from concrete Adapters.
func Read(adapters []ReadAdapter, options ...DefinitionOption) (agent.ToolDefinition, error) {
	descriptor := readDescriptor(options...)
	tool, err := newReadTool(adapters, descriptor.MaxResultBytes)
	if err != nil {
		return agent.ToolDefinition{}, err
	}
	identities := make([]agent.CapabilityIdentity, len(adapters))
	for index := range adapters {
		identities[index] = adapters[index].Identity()
	}
	return agent.ToolDefinition{
		Tool: tool, Descriptor: descriptor,
		ImplementationIdentity: toolsetIdentity("tools.read", struct {
			Contract int
			Adapters []agent.CapabilityIdentity
		}{Contract: readToolContractVersion, Adapters: identities}),
	}, nil
}

func newReadTool(adapters []ReadAdapter, maxResultBytes int) (*readTool, error) {
	if len(adapters) == 0 {
		return nil, errors.New("read requires at least one adapter")
	}
	seen := make(map[string]struct{}, len(adapters))
	selected := make([]ReadAdapter, 0, len(adapters))
	names := make([]string, 0, len(adapters))
	for _, adapter := range adapters {
		if adapter == nil {
			return nil, errors.New("read adapter is nil")
		}
		if err := validateAdapterIdentity("read Adapter", adapter.Identity()); err != nil {
			return nil, err
		}
		name := strings.TrimSpace(adapter.Name())
		if name == "" {
			return nil, errors.New("read adapter has no stable name")
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, fmt.Errorf("duplicate read adapter %q", name)
		}
		parameters := adapter.Parameters()
		if parameters == nil {
			return nil, fmt.Errorf("read adapter %q has no parameter schema", name)
		}
		seen[name] = struct{}{}
		selected = append(selected, adapter)
		names = append(names, name)
	}
	schema, err := readAdapterSchema(selected)
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	return &readTool{
		adapters:       selected,
		schema:         schema,
		maxResultBytes: maxResultBytes,
		desc: "Read one bounded resource using the parameters supported by its registered adapter. " +
			"HTTP(S) URLs are not supported; use web_fetch. Available adapters: " + strings.Join(names, ", ") + ".",
	}, nil
}

func (tool *readTool) Info(context.Context) (*agent.ToolInfo, error) {
	if tool == nil || tool.schema == nil {
		return nil, errors.New("read tool is not configured")
	}
	return &agent.ToolInfo{Name: "read", Desc: tool.desc, ParamsOneOf: agent.NewParamsOneOfByJSONSchema(tool.schema)}, nil
}

func (tool *readTool) Run(ctx context.Context, arguments string, _ ...agent.ToolOption) (agent.ToolResult, error) {
	info, err := tool.Info(ctx)
	if err != nil {
		return agent.ToolResult{}, err
	}
	normalizedArguments, err := agent.NormalizeToolArguments(info, arguments)
	if err != nil {
		return agent.ToolResult{}, fmt.Errorf("decode read arguments: %w", err)
	}
	arguments = normalizedArguments
	resourcePath, err := readPathArgument(arguments)
	if err != nil {
		return agent.ToolResult{}, err
	}
	if strings.HasPrefix(strings.ToLower(resourcePath), "http://") || strings.HasPrefix(strings.ToLower(resourcePath), "https://") {
		return agent.ToolResult{}, errors.New("read does not support HTTP(S) resources; use web_fetch")
	}
	var matched ReadAdapter
	var matchErr error
	var notFoundErr error
	for _, adapter := range tool.adapters {
		owns, err := adapter.Match(ctx, resourcePath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				if notFoundErr == nil {
					notFoundErr = err
				}
			} else if matchErr == nil {
				matchErr = err
			}
			continue
		}
		if !owns {
			continue
		}
		if matched != nil {
			return agent.ToolResult{}, fmt.Errorf("read path %q is ambiguous between adapters %q and %q", resourcePath, matched.Name(), adapter.Name())
		}
		matched = adapter
	}
	if matched == nil {
		if matchErr != nil {
			return agent.ToolResult{}, matchErr
		}
		if notFoundErr != nil {
			return projectReadNotFound(resourcePath, "unresolved", notFoundErr, tool.maxResultBytes), nil
		}
		return agent.ToolResult{}, fmt.Errorf("no read adapter accepts path %q", resourcePath)
	}
	result, err := matched.Read(ctx, arguments)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return projectReadNotFound(resourcePath, matched.Name(), err, tool.maxResultBytes), nil
		}
		return agent.ToolResult{}, err
	}
	if strings.TrimSpace(result.Path) == "" {
		result.Path = resourcePath
	}
	if strings.TrimSpace(result.Kind) == "" {
		result.Kind = matched.Name()
	}
	return projectReadResult(result, tool.maxResultBytes)
}

// projectReadNotFound keeps resource absence as a successful observation from
// the read operation. Agents need the original filesystem diagnostic to reason
// about optional or not-yet-created paths, while actual inspection failures
// such as permission errors must still use the tool error channel.
func projectReadNotFound(resourcePath, kind string, cause error, maxResultBytes int) agent.ToolResult {
	diagnostic := strings.ToValidUTF8(cause.Error(), "\uFFFD")
	metadata := readEnvelope{
		Schema: "resource.read.v1", Status: "not_found",
		Source: readSource{Kind: kind, Path: resourcePath},
		Limits: readLimits{Truncated: false},
	}
	modelContent := diagnostic
	var details json.RawMessage
	encoded, err := json.Marshal(metadata)
	if err == nil {
		details = encoded
		candidate := string(encoded) + "\n" + diagnostic
		if maxResultBytes <= 0 || len(candidate) <= maxResultBytes {
			modelContent = candidate
		}
		if maxResultBytes > 0 && len(details) > maxResultBytes {
			details = nil
		}
	}
	if maxResultBytes > 0 && len(modelContent) > maxResultBytes {
		modelContent = truncateUTF8(modelContent, maxResultBytes)
	}
	return agent.ToolResult{
		ModelContent: modelContent, DisplayContent: modelContent, Details: details,
		Status: agent.ToolResultSuccess,
	}
}

func projectReadResult(result ReadResult, maxResultBytes int) (agent.ToolResult, error) {
	lines := splitResultLines(result.Content)
	returned := result.Limit
	if returned <= 0 || returned > len(lines) {
		returned = len(lines)
	}
	build := func(visible int) (agent.ToolResult, ReadResult, error) {
		projected := result
		projected.Content = strings.Join(lines[:visible], "")
		projected.Limit = visible
		projected.Truncated = result.Truncated || visible < returned
		if visible < returned && projected.Offset > 0 {
			projected.NextOffset = projected.Offset + visible
			projected.NextByteOffset = 0
		} else if !projected.Truncated {
			projected.NextOffset = 0
			projected.NextByteOffset = 0
		}
		content := projected.Content
		if projected.Offset > 0 {
			content = lineNumbers(content, projected.Offset)
		}
		metadata := readResultEnvelope(projected)
		encoded, err := json.Marshal(metadata)
		if err != nil {
			return agent.ToolResult{}, ReadResult{}, fmt.Errorf("serialize read result: %w", err)
		}
		modelContent := string(encoded)
		if content != "" {
			modelContent += "\n" + content
		}
		return agent.ToolResult{
			ModelContent: modelContent, DisplayContent: modelContent, Details: encoded,
			Status: agent.ToolResultSuccess,
		}, projected, nil
	}
	full, _, err := build(returned)
	if err != nil {
		return agent.ToolResult{}, err
	}
	if len(full.ModelContent) <= maxResultBytes && len(full.Details) <= maxResultBytes {
		return full, nil
	}
	low, high := 0, returned-1
	var best agent.ToolResult
	bestVisible := -1
	for low <= high {
		middle := low + (high-low)/2
		candidate, _, err := build(middle)
		if err != nil {
			return agent.ToolResult{}, err
		}
		if len(candidate.ModelContent) <= maxResultBytes && len(candidate.Details) <= maxResultBytes {
			best, bestVisible = candidate, middle
			low = middle + 1
		} else {
			high = middle - 1
		}
	}
	if bestVisible < 0 {
		return agent.ToolResult{}, fmt.Errorf("read result metadata exceeds the %d-byte result limit", maxResultBytes)
	}
	if returned > 0 && bestVisible == 0 {
		fragment, err := projectReadLineFragment(result, lines[0], maxResultBytes)
		if err != nil {
			return agent.ToolResult{}, err
		}
		return fragment, nil
	}
	return best, nil
}

func projectReadLineFragment(result ReadResult, line string, maxResultBytes int) (agent.ToolResult, error) {
	build := func(visibleBytes int) (agent.ToolResult, error) {
		content := line[:visibleBytes]
		projected := result
		projected.Content = content
		projected.Limit = 1
		projected.Truncated = true
		projected.NextOffset = result.Offset
		projected.NextByteOffset = result.ByteOffset + visibleBytes
		metadata := readResultEnvelope(projected)
		encoded, err := json.Marshal(metadata)
		if err != nil {
			return agent.ToolResult{}, fmt.Errorf("serialize read result: %w", err)
		}
		modelContent := string(encoded)
		if content != "" {
			modelContent += "\n" + lineNumbers(content, projected.Offset)
		}
		return agent.ToolResult{
			ModelContent: modelContent, DisplayContent: modelContent, Details: encoded,
			Status: agent.ToolResultSuccess,
		}, nil
	}
	low, high := 1, len(line)-1
	bestBytes := 0
	var best agent.ToolResult
	for low <= high {
		probe := low + (high-low)/2
		middle := probe
		for middle > 0 && !utf8.ValidString(line[:middle]) {
			middle--
		}
		if middle == 0 {
			low = probe + 1
			continue
		}
		candidate, err := build(middle)
		if err != nil {
			return agent.ToolResult{}, err
		}
		if len(candidate.ModelContent) <= maxResultBytes && len(candidate.Details) <= maxResultBytes {
			best, bestBytes = candidate, middle
			low = probe + 1
		} else {
			high = probe - 1
		}
	}
	if bestBytes == 0 {
		return agent.ToolResult{}, fmt.Errorf("read result metadata leaves no room for a UTF-8 line fragment within the %d-byte result limit", maxResultBytes)
	}
	return best, nil
}

func splitResultLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.SplitAfter(content, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

type readSource struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

type readLimits struct {
	Offset         int    `json:"offset,omitempty"`
	ByteOffset     int    `json:"byte_offset,omitempty"`
	Returned       int    `json:"returned,omitempty"`
	Total          int    `json:"total,omitempty"`
	Truncated      bool   `json:"truncated"`
	NextOffset     int    `json:"next_offset,omitempty"`
	NextByteOffset int    `json:"next_byte_offset,omitempty"`
	Unit           string `json:"unit,omitempty"`
}

type readRecovery struct {
	Retryable  bool   `json:"retryable"`
	Suggestion string `json:"suggestion"`
}

type readEnvelope struct {
	Schema   string        `json:"schema"`
	Status   string        `json:"status"`
	Source   readSource    `json:"source"`
	Limits   readLimits    `json:"limits"`
	Recovery *readRecovery `json:"recovery,omitempty"`
}

func readResultEnvelope(result ReadResult) readEnvelope {
	status := "success"
	var recovery *readRecovery
	if result.Truncated {
		status = "partial"
		suggestion := "Narrow the resource path or requested limit."
		if result.NextOffset > 0 && result.NextByteOffset > 0 {
			suggestion = fmt.Sprintf("Continue with offset=%d and byte_offset=%d.", result.NextOffset, result.NextByteOffset)
		} else if result.NextOffset > 0 {
			suggestion = fmt.Sprintf("Continue with offset=%d.", result.NextOffset)
		}
		recovery = &readRecovery{Retryable: true, Suggestion: suggestion}
	}
	return readEnvelope{
		Schema: "resource.read.v1", Status: status,
		Source: readSource{Kind: result.Kind, Path: result.Path},
		Limits: readLimits{
			Offset: result.Offset, ByteOffset: result.ByteOffset, Returned: result.Limit, Total: result.Total,
			Truncated: result.Truncated, NextOffset: result.NextOffset, NextByteOffset: result.NextByteOffset, Unit: result.Unit,
		},
		Recovery: recovery,
	}
}

// readAdapterSchema exposes one parameter object to providers. Only fields
// required by every adapter are globally required; the selected adapter still
// owns its full contract. Shared fields must have identical structural meaning.
func readAdapterSchema(adapters []ReadAdapter) (*jsonschema.Schema, error) {
	merged, err := reflectedToolSchema[struct{}]()
	if err != nil {
		return nil, err
	}
	type propertyContract struct {
		adapter      string
		schema       string
		parameter    *jsonschema.Schema
		required     bool
		count        int
		descriptions []string
	}
	contracts := make(map[string]propertyContract)
	for _, adapter := range adapters {
		schema := adapter.Parameters()
		if schema == nil || schema.Type != "object" || schema.Properties == nil || len(schema.OneOf) != 0 || len(schema.AnyOf) != 0 || len(schema.AllOf) != 0 {
			return nil, fmt.Errorf("read adapter %q must expose object properties directly", adapter.Name())
		}
		required := make(map[string]bool, len(schema.Required))
		for _, name := range schema.Required {
			required[name] = true
		}
		for pair := schema.Properties.Oldest(); pair != nil; pair = pair.Next() {
			encoded, err := canonicalReadPropertySchema(pair.Value)
			if err != nil {
				return nil, fmt.Errorf("canonicalize read adapter %q parameter %q: %w", adapter.Name(), pair.Key, err)
			}
			contract, exists := contracts[pair.Key]
			if exists && (contract.schema != encoded || contract.required != required[pair.Key]) {
				return nil, fmt.Errorf("read parameter %q has conflicting contracts in adapters %q and %q", pair.Key, contract.adapter, adapter.Name())
			}
			if !exists {
				contract = propertyContract{adapter: adapter.Name(), schema: encoded, parameter: pair.Value, required: required[pair.Key]}
			}
			contract.count++
			if pair.Value != nil && pair.Value.Description != "" {
				contract.descriptions = append(contract.descriptions, adapter.Name()+": "+pair.Value.Description)
			}
			contracts[pair.Key] = contract
		}
	}
	names := make([]string, 0, len(contracts))
	for name := range contracts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		contract := contracts[name]
		if contract.parameter != nil {
			sort.Strings(contract.descriptions)
			contract.parameter.Description = strings.Join(contract.descriptions, "\n")
		}
		merged.Properties.Set(name, contract.parameter)
		if contract.required && contract.count == len(adapters) {
			merged.Required = append(merged.Required, name)
		}
	}
	return merged, nil
}

func canonicalReadPropertySchema(schema *jsonschema.Schema) (string, error) {
	if schema == nil {
		return "null", nil
	}
	clone := agent.NewParamsOneOfByJSONSchema(schema)
	materialized, err := clone.ToJSONSchema()
	if err != nil {
		return "", err
	}
	materialized.Title = ""
	materialized.Description = ""
	encoded, err := json.Marshal(materialized)
	return string(encoded), err
}

func readPathArgument(arguments string) (string, error) {
	decoder := json.NewDecoder(strings.NewReader(arguments))
	decoder.UseNumber()
	var input map[string]any
	if err := decoder.Decode(&input); err != nil {
		return "", fmt.Errorf("decode read path: %w", err)
	}
	if input == nil {
		return "", errors.New("read arguments must be an object")
	}
	value, _ := input["path"].(string)
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("read path is required")
	}
	return value, nil
}
