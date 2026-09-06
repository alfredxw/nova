import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { APIError } from '@/lib/api-client'
import { queryClient } from '@/lib/query-client'
import { GLOBAL_SETTINGS_TARGET, settingsQueryKeys, subscribeSettingsTarget } from '@/features/settings/query'
import { fetchConversationConfig, patchConversationConfig } from './api'
import type {
  ConversationConfigBinding,
  ConversationConfigChanges,
  ConversationConfigController,
  ConversationConfigSnapshot,
} from './types'

const CONVERSATION_CONFIG_UPDATED_EVENT = 'nova:conversation-config-updated'
let nextConversationConfigSourceID = 1

interface ConversationConfigUpdatedDetail {
  bindingKey: string
  source: string
  snapshot: ConversationConfigSnapshot
}

interface BoundSnapshot {
  bindingKey: string
  snapshot: ConversationConfigSnapshot
}

interface BoundError {
  bindingKey: string
  message: string
}

/**
 * Owns one conversation's compare-and-swap configuration snapshot.
 * The backend also remembers explicit Writing/Game model choices in Settings.
 * Existing conversations keep their own snapshots; uncommitted drafts refresh.
 */
export function useConversationConfig(binding?: ConversationConfigBinding): ConversationConfigController {
  const normalizedBinding = useMemo(
    () => binding ? normalizeBinding(binding) : undefined,
    [
      binding?.branch_id,
      binding?.mode,
      binding?.origin,
      binding?.project_id,
      binding?.resource_id,
      binding?.run_id,
      binding?.session_id,
      binding?.story_id,
    ],
  )
  const bindingKey = normalizedBinding ? conversationConfigBindingKey(normalizedBinding) : ''
  const [source] = useState(() => `conversation-config-${nextConversationConfigSourceID++}`)
  const [boundSnapshot, setBoundSnapshot] = useState<BoundSnapshot | null>(null)
  const [loading, setLoading] = useState(Boolean(normalizedBinding))
  const [saving, setSaving] = useState(false)
  const [boundError, setBoundError] = useState<BoundError | null>(null)
  const generationRef = useRef(0)
  const savingRef = useRef(false)
  // A React state value survives a prop change until its effect runs. Scope the
  // snapshot synchronously so the previous conversation can never remain
  // actionable while the next binding is loading.
  const snapshot = boundSnapshot?.bindingKey === bindingKey ? boundSnapshot.snapshot : null
  const error = boundError?.bindingKey === bindingKey ? boundError.message : null

  const reload = useCallback(async () => {
    const generation = ++generationRef.current
    if (!normalizedBinding) {
      setBoundSnapshot(null)
      setLoading(false)
      setBoundError(null)
      return null
    }
    setLoading(true)
    try {
      const next = await fetchConversationConfig(normalizedBinding)
      if (generation !== generationRef.current) return null
      setBoundSnapshot({ bindingKey, snapshot: next })
      setBoundError(null)
      return next
    } catch (reason) {
      if (generation !== generationRef.current) return null
      const message = errorMessage(reason)
      console.warn('[conversation-config] load failed', { binding: normalizedBinding, reason })
      setBoundSnapshot(null)
      setBoundError({ bindingKey, message })
      return null
    } finally {
      if (generation === generationRef.current) setLoading(false)
    }
  }, [bindingKey, normalizedBinding])

  useEffect(() => {
    savingRef.current = false
    setSaving(false)
    void reload()
    return () => { generationRef.current += 1 }
  }, [reload])

  useEffect(() => {
    if (!bindingKey) return
    const synchronize = (event: Event) => {
      const detail = (event as CustomEvent<ConversationConfigUpdatedDetail>).detail
      if (!detail || detail.source === source || detail.bindingKey !== bindingKey) return
      setBoundSnapshot({ bindingKey, snapshot: detail.snapshot })
      setBoundError(null)
    }
    window.addEventListener(CONVERSATION_CONFIG_UPDATED_EVENT, synchronize)
    return () => window.removeEventListener(CONVERSATION_CONFIG_UPDATED_EVENT, synchronize)
  }, [bindingKey, source])

  useEffect(() => {
    if (snapshot?.revision !== 0) return
    return subscribeSettingsTarget(GLOBAL_SETTINGS_TARGET, () => { void reload() })
  }, [reload, snapshot?.revision])

  const patch = useCallback(async (changes: ConversationConfigChanges) => {
    if (!normalizedBinding || savingRef.current || !hasChanges(changes)) return false
    let base = snapshot
    if (!base) base = await reload()
    if (!base || savingRef.current) return false

    savingRef.current = true
    setSaving(true)
    setBoundError(null)
    try {
      let next: ConversationConfigSnapshot
      try {
        next = await patchConversationConfig(normalizedBinding, changes, base.revision)
      } catch (reason) {
        if (!(reason instanceof APIError) || reason.status !== 409) throw reason
        const latest = await fetchConversationConfig(normalizedBinding)
        next = await patchConversationConfig(normalizedBinding, changes, latest.revision)
      }
      setBoundSnapshot({ bindingKey, snapshot: next })
      window.dispatchEvent(new CustomEvent<ConversationConfigUpdatedDetail>(CONVERSATION_CONFIG_UPDATED_EVENT, {
        detail: { bindingKey, source, snapshot: next },
      }))
      return true
    } catch (reason) {
      const message = errorMessage(reason)
      console.warn('[conversation-config] save failed', { binding: normalizedBinding, changes, reason })
      // The conversation can be durable even if remembering the new default
      // failed. Show its actual selection while retaining the save error.
      try {
        const current = await fetchConversationConfig(normalizedBinding)
        setBoundSnapshot({ bindingKey, snapshot: current })
      } catch (reloadError) {
        console.warn('[conversation-config] reload after save failure failed', { binding: normalizedBinding, reloadError })
      }
      setBoundError({ bindingKey, message })
      return false
    } finally {
      savingRef.current = false
      setSaving(false)
      if ((changes.profile_id !== undefined || changes.thinking_level !== undefined)
        && (base.agent_kind === 'ide' || base.agent_kind === 'interactive_story')) {
        void queryClient.invalidateQueries({ queryKey: settingsQueryKeys.all, refetchType: 'all' })
          .catch((reason) => console.warn('[conversation-config] refresh remembered model settings failed', { reason }))
      }
    }
  }, [bindingKey, normalizedBinding, reload, snapshot, source])

  return {
    snapshot,
    initialized: snapshot !== null,
    loading: Boolean(normalizedBinding) && (loading || (snapshot === null && error === null)),
    saving,
    error,
    patch,
    reload,
  }
}

export function conversationConfigBindingKey(binding: ConversationConfigBinding) {
  return [
    binding.mode,
    binding.project_id ?? '',
    binding.session_id ?? '',
    binding.story_id ?? '',
    binding.branch_id ?? '',
    binding.origin ?? '',
    binding.resource_id ?? '',
    binding.run_id ?? '',
  ].join('\u001f')
}

function normalizeBinding(binding: ConversationConfigBinding): ConversationConfigBinding {
  return {
    mode: binding.mode,
    ...(binding.project_id?.trim() ? { project_id: binding.project_id.trim() } : {}),
    ...(binding.session_id?.trim() ? { session_id: binding.session_id.trim() } : {}),
    ...(binding.story_id?.trim() ? { story_id: binding.story_id.trim() } : {}),
    ...(binding.branch_id?.trim() ? { branch_id: binding.branch_id.trim() } : {}),
    ...(binding.origin?.trim() ? { origin: binding.origin.trim() } : {}),
    ...(binding.resource_id?.trim() ? { resource_id: binding.resource_id.trim() } : {}),
    ...(binding.run_id?.trim() ? { run_id: binding.run_id.trim() } : {}),
  }
}

function hasChanges(changes: ConversationConfigChanges) {
  return Object.values(changes).some((value) => value !== undefined)
}

function errorMessage(reason: unknown) {
  return reason instanceof Error ? reason.message : String(reason || 'Unknown error')
}
