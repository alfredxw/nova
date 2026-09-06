import { useCallback, useEffect, useMemo, useState } from 'react'
import { Check, ChevronDown, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { fetchSettings } from '@/features/settings/api'
import { GLOBAL_SETTINGS_TARGET, subscribeSettingsTarget } from '@/features/settings/query'
import { buildModelProfileOptions, type ModelProfileOption } from '@/features/settings/model-profile-options'
import type { LayeredSettings } from '@/features/settings/types'
import { normalizeThinkingLevel, THINKING_LEVELS, type ThinkingLevel } from '@/features/settings/thinking-levels'
import type { VisibleAgentKey } from '@/features/agents/agent-registry'
import type { ConversationConfigController } from '@/features/conversation-config/types'

interface ModelProfileSwitcherProps {
  agentKey?: VisibleAgentKey
  workspace?: string
  conversationConfig?: ConversationConfigController
  disabled?: boolean
  runActive?: boolean
}

interface SavingSelection {
  kind: 'profile' | 'thinking'
  value: string
}

// User-selected fonts can paint beyond their line box. Clip model labels only
// horizontally so ellipsis still works without cutting off glyphs vertically.
const MODEL_LABEL_OVERFLOW_CLASS = 'min-w-0 overflow-x-clip overflow-y-visible text-ellipsis whitespace-nowrap'

export function ModelProfileSwitcher({ agentKey, workspace, conversationConfig, disabled = false, runActive = false }: ModelProfileSwitcherProps) {
  const selector = useModelProfileSelector({ agentKey, workspace, conversationConfig, disabled })
  const [open, setOpen] = useState(false)

  if (!selector.enabled) return null

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          disabled={disabled || !selector.settings || !conversationConfig?.initialized || selector.saving}
          className="group flex h-8 min-w-0 max-w-44 flex-[0_1_auto] items-center gap-1.5 rounded-md border-0 bg-transparent px-1.5 text-xs leading-none text-[var(--nova-text)] outline-none transition-colors hover:text-[var(--nova-text)] focus-visible:bg-[var(--nova-hover)] disabled:pointer-events-none disabled:opacity-50"
          aria-label={selector.t('chat.modelProfile.switch', { model: selector.currentSelectionLabel })}
          data-model-profile-trigger="true"
          data-current-model={selector.currentModelLabel}
          data-current-thinking-level={selector.currentThinkingLevel}
        >
          <span className={MODEL_LABEL_OVERFLOW_CLASS}>{selector.settings ? selector.currentModelLabel : selector.t('chat.modelProfile.loading')}</span>
          {selector.currentThinkingLevelLabel ? (
            <span className="shrink-0 font-normal text-[var(--nova-text-faint)]">{selector.currentThinkingLevelLabel}</span>
          ) : null}
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-[var(--nova-text-faint)] transition-transform group-data-[state=open]:rotate-180" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        side="top"
        aria-label={selector.t('chat.modelProfile.action')}
        className="w-60 border-[var(--nova-border)] bg-[var(--nova-surface-2)] p-1.5 text-[var(--nova-text)]"
      >
        {agentKey === 'ide' || agentKey === 'interactive_story' ? (
          <div className="px-1.5 py-1 text-[11px] leading-4 text-[var(--nova-text-faint)]">
            {selector.t('chat.modelProfile.rememberSelection')}
          </div>
        ) : null}
        {runActive ? (
          <>
            <div role="note" className="px-1.5 py-1 text-[11px] leading-4 text-[var(--nova-text-faint)]">
              {selector.t('chat.input.changesApplyNextTurn')}
            </div>
            <DropdownMenuSeparator className="bg-[var(--nova-border-soft)]" />
          </>
        ) : null}
        <ModelProfileOptions
          selector={selector}
          onThinkingLevelSelect={(level) => {
            setOpen(false)
            void selector.selectThinkingLevel(level)
          }}
        />
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
interface ModelProfileSelectorInput extends ModelProfileSwitcherProps {}

interface ModelProfileSelector {
  t: (key: string, options?: Record<string, unknown>) => string
  enabled: boolean
  settings: LayeredSettings | null
  options: ModelProfileOption[]
  currentProfile: string
  currentModelLabel: string
  currentThinkingLevel: ThinkingLevel | ''
  currentThinkingLevelLabel: string
  currentSelectionLabel: string
  savingSelection: SavingSelection | null
  error: string | null
  selectProfile: (profileID: string) => Promise<void>
  saving: boolean
  selectThinkingLevel: (level: ThinkingLevel) => Promise<void>
}

function useModelProfileSelector({ agentKey, conversationConfig, disabled = false }: ModelProfileSelectorInput): ModelProfileSelector {
  const { t } = useTranslation()
  const [settings, setSettings] = useState<LayeredSettings | null>(null)
  const [savingSelection, setSavingSelection] = useState<SavingSelection | null>(null)
  const [catalogError, setCatalogError] = useState<string | null>(null)
  // Model profiles are user-scoped. Global conversations (notably user-wide
  // automations) therefore remain configurable without a workspace path.
  const enabled = Boolean(agentKey && conversationConfig)

  const load = useCallback(() => {
    if (!enabled) {
      setSettings(null)
      return
    }
    fetchSettings()
      .then((next) => {
        setSettings(next)
        setCatalogError(null)
      })
      .catch((err) => {
        setCatalogError(err instanceof Error ? err.message : t('chat.modelProfile.loadFailed'))
      })
  }, [enabled, t])

  useEffect(() => {
    load()
  }, [load])

  useEffect(() => {
    if (!enabled) return
    return subscribeSettingsTarget(GLOBAL_SETTINGS_TARGET, (snapshot) => {
      setSettings(snapshot)
      setCatalogError(null)
    })
  }, [enabled])

  const options = useMemo(
    () => buildModelProfileOptions(settings, t),
    [settings, t],
  )
  const currentProfile = conversationConfig?.snapshot?.profile_id || 'default'
  const currentModelLabel = options.find((option) => option.id === currentProfile)?.modelLabel || currentProfile
  const currentThinkingLevel = normalizeThinkingLevel(conversationConfig?.snapshot?.thinking_level) ?? ''
  const currentThinkingLevelLabel = currentThinkingLevel
    ? t(`chat.modelProfile.thinking.${currentThinkingLevel}`)
    : ''
  const currentSelectionLabel = [currentModelLabel, currentThinkingLevelLabel].filter(Boolean).join(' ')

  const saveConversationSelection = async (selection: SavingSelection) => {
    if (!conversationConfig || disabled || conversationConfig.saving) return
    setSavingSelection(selection)
    try {
      const saved = await conversationConfig.patch(selection.kind === 'profile'
        ? { profile_id: selection.value }
        : { thinking_level: selection.value as ThinkingLevel })
      if (!saved) toast.error(t('chat.modelProfile.saveFailed'))
    } finally {
      setSavingSelection(null)
    }
  }

  const selectProfile = async (profileID: string) => {
    if (!conversationConfig) return
    await saveConversationSelection({ kind: 'profile', value: profileID })
  }

  const selectThinkingLevel = async (level: ThinkingLevel) => {
    if (!conversationConfig) return
    await saveConversationSelection({ kind: 'thinking', value: level })
  }

  return {
    t,
    enabled,
    settings,
    options,
    currentProfile,
    currentModelLabel,
    currentThinkingLevel,
    currentThinkingLevelLabel,
    currentSelectionLabel,
    savingSelection,
    error: catalogError || conversationConfig?.error || null,
    saving: conversationConfig?.saving ?? false,
    selectProfile,
    selectThinkingLevel,
  }
}

function ModelProfileOptions({
  selector,
  onThinkingLevelSelect,
}: {
  selector: ModelProfileSelector
  onThinkingLevelSelect: (level: ThinkingLevel) => void
}) {
  const {
    t,
    options,
    currentProfile,
    currentThinkingLevel,
    savingSelection,
    error,
    selectProfile,
  } = selector
  return (
    <>
      <div className="px-1.5 pb-1 pt-0.5 text-[10px] font-medium text-[var(--nova-text-faint)]">
        {t('chat.modelProfile.modelSection')}
      </div>
      {options.map((option) => (
        <DropdownMenuItem
          key={option.id}
          disabled={Boolean(savingSelection)}
          onSelect={() => void selectProfile(option.id)}
          className="cursor-pointer py-1.5 text-xs focus:bg-[var(--nova-active)] focus:text-[var(--nova-text)]"
        >
          {savingSelection?.kind === 'profile' && savingSelection.value === option.id
            ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
            : <Check className={`h-3.5 w-3.5 ${option.id === currentProfile ? 'opacity-100' : 'opacity-0'}`} />}
          <span className={`${MODEL_LABEL_OVERFLOW_CLASS} flex-1`}>{option.label}</span>
        </DropdownMenuItem>
      ))}
      {options.length === 0 ? (
        <DropdownMenuItem disabled className="text-xs">
          {t('chat.modelProfile.empty')}
        </DropdownMenuItem>
      ) : null}
      <DropdownMenuSeparator className="bg-[var(--nova-border-soft)]" />
      <div className="px-1.5 pb-1 pt-0.5 text-[10px] font-medium text-[var(--nova-text-faint)]">
        {t('chat.modelProfile.thinkingSection')}
      </div>
      <div
        role="group"
        aria-label={t('chat.modelProfile.thinkingSection')}
        className="grid grid-cols-4 gap-1 px-1 pb-1"
      >
        {THINKING_LEVELS.map((level) => {
          const selected = level === currentThinkingLevel
          const label = t(`chat.modelProfile.thinking.${level}`)
          return (
            <button
              key={level}
              type="button"
              disabled={Boolean(savingSelection)}
              aria-pressed={selected}
              onClick={() => onThinkingLevelSelect(level)}
              className={`flex h-7 min-w-0 items-center justify-center rounded-md border px-1 text-[11px] transition-colors disabled:opacity-50 ${
                selected
                  ? 'border-[var(--nova-border)] bg-[var(--nova-active)] text-[var(--nova-text)]'
                  : 'border-transparent text-[var(--nova-text-muted)] hover:bg-[var(--nova-hover)] hover:text-[var(--nova-text)]'
              }`}
            >
              {savingSelection?.kind === 'thinking' && savingSelection.value === level
                ? <Loader2 className="h-3.5 w-3.5 animate-spin" />
                : <span className="truncate">{label}</span>}
            </button>
          )
        })}
      </div>
      {error ? (
        <>
          <DropdownMenuSeparator className="bg-[var(--nova-border-soft)]" />
          <DropdownMenuItem disabled className="text-xs text-red-400">
            {error}
          </DropdownMenuItem>
        </>
      ) : null}
    </>
  )
}
