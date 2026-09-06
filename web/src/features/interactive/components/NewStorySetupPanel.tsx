import { ChevronDown, Play, SlidersHorizontal } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Field, FieldDescription, FieldLabel } from '@/components/ui/field'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import type { LoreItem } from '@/lib/api'
import { cn } from '@/lib/utils'
import { hasLoreProtagonistTag } from '@/features/lore/tags'
import type { ConversationConfigChanges, ConversationConfigController, ConversationConfigSnapshot } from '@/features/conversation-config/types'
import { normalizeThinkingLevel } from '@/features/settings/thinking-levels'
import { normalizeStoryCheckSettings } from '../check-settings'
import { gamePlanningTemplateName } from '../game-planning'
import { normalizeStoryImageSettings } from '../image-settings'
import { DEFAULT_NARRATIVE_STYLE_ID, resolveNarrativeStyle } from '../narrative-style'
import { DEFAULT_INTERACTIVE_CHOICE_COUNT, DEFAULT_INTERACTIVE_REPLY_TARGET_CHARS, truncateStoryOpeningText, type BookOpeningPreset, type StoryCreateInput } from '../opening'
import type { GamePlanningTemplate, ImagePreset, StoryOpeningConfig, StoryProtagonist, StorySummary, Teller } from '../types'
import { StoryOpeningSelector } from './story-setup/StoryOpeningSelector'
import { StoryProtagonistSelector } from './story-setup/StoryProtagonistSelector'
import { StorySetupAdvanced, type StorySetupSettings } from './story-setup/StorySetupAdvanced'

interface NewStorySetupPanelProps {
  projectId: string
  tellers: Teller[]
  planningTemplates: GamePlanningTemplate[]
  imagePresets: ImagePreset[]
  loreItems?: LoreItem[]
  bookOpeningPresets?: BookOpeningPreset[]
  recentNarrativeStyleID?: string
  narrativeStyleLoading?: boolean
  conversationConfig: ConversationConfigController
  story?: StorySummary
  onNarrativeStyleChange?: (id: string) => void | Promise<unknown>
  onRequestLoreInit?: () => void
  onOpenPresets?: () => void
  onCancel: () => void
  onCreate: (input: StoryCreateInput) => void | Promise<void>
}

export function NewStorySetupPanel({
  projectId,
  tellers,
  planningTemplates,
  imagePresets,
  loreItems = [],
  bookOpeningPresets = [],
  recentNarrativeStyleID = DEFAULT_NARRATIVE_STYLE_ID,
  narrativeStyleLoading = false,
  conversationConfig,
  story,
  onNarrativeStyleChange,
  onRequestLoreInit,
  onOpenPresets,
  onCancel,
  onCreate,
}: NewStorySetupPanelProps) {
  const { t } = useTranslation()
  const initialTemplate = planningTemplates.find((item) => item.id === story?.planning_template_id) || planningTemplates[0]
  const recentTeller = resolveNarrativeStyle(tellers, recentNarrativeStyleID)
  const initialProtagonist = story?.protagonist || defaultStoryProtagonist(loreItems)
  const [planningTemplateId, setPlanningTemplateId] = useState(initialTemplate?.id || 'default')
  const [protagonist, setProtagonist] = useState<StoryProtagonist>(initialProtagonist)
  const [opening, setOpening] = useState<StoryOpeningConfig>(() => story?.opening || { mode: 'custom' })
  const [settings, setSettings] = useState<StorySetupSettings>(() => initialSettings(story, recentTeller?.id, conversationConfig.snapshot))
  const [advancedOpen, setAdvancedOpen] = useState(true)
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState('')
  const initialProtagonistRef = useRef<StoryProtagonist>(initialProtagonist)
  const protagonistSelectionTouchedRef = useRef(false)
  const narrativeStyleSelectionLockedRef = useRef(Boolean(story))
  const planningTemplate = planningTemplates.find((item) => item.id === planningTemplateId) || planningTemplates[0]
  const planningTemplateName = planningTemplate ? gamePlanningTemplateName(planningTemplate, t) : planningTemplateId
  const advancedSummary = useMemo(() => t('storyPicker.setup.advanced.summary', {
    planning: settings.planningEnabled ? t('storyPicker.setup.advanced.planningOn') : t('storyPicker.setup.advanced.planningOff'),
    checks: settings.moduleRefs.rule_system_disabled ? t('storyPicker.setup.advanced.checksOff') : t('storyPicker.setup.advanced.checksOn'),
    images: settings.imageSettings.mode === 'interval' ? t('storyPicker.setup.advanced.imagesOn') : t('storyPicker.setup.advanced.imagesOff'),
  }), [settings.imageSettings.mode, settings.moduleRefs.rule_system_disabled, settings.planningEnabled, t])
  const runtimeConfigLoading = conversationConfig.loading || (!conversationConfig.error && !settings.modelProfileId)
  const runtimeConfigReady = conversationConfig.initialized && Boolean(settings.modelProfileId)
  const startButtonLoading = creating || narrativeStyleLoading || runtimeConfigLoading
  let startButtonLabel = t('storyPicker.setup.start')
  if (narrativeStyleLoading || runtimeConfigLoading) startButtonLabel = t('common.loading')
  if (creating) startButtonLabel = t('storyPicker.setup.starting')

  useEffect(() => {
    if (story || narrativeStyleSelectionLockedRef.current || !recentTeller) return
    setSettings((current) => ({
      ...current,
      moduleRefs: { ...current.moduleRefs, narrative_style_id: recentTeller.id },
    }))
  }, [recentTeller, story])

  useEffect(() => {
    const snapshot = conversationConfig.snapshot
    if (!snapshot) return
    setSettings((current) => ({
      ...current,
      modelProfileId: snapshot.profile_id || 'default',
      thinkingLevel: normalizeThinkingLevel(snapshot.thinking_level) || 'default',
    }))
  }, [conversationConfig.snapshot])

  useEffect(() => {
    if (protagonistSelectionTouchedRef.current || protagonist.mode !== 'default') return
    const tagged = defaultStoryProtagonist(loreItems)
    if (tagged.mode === 'lore') setProtagonist(tagged)
  }, [loreItems, protagonist.mode])

  const changeProtagonist = (next: StoryProtagonist) => {
    protagonistSelectionTouchedRef.current = true
    setProtagonist(next)
  }

  const changeSettings = async (next: StorySetupSettings) => {
    const changes: ConversationConfigChanges = {}
    if (next.modelProfileId !== settings.modelProfileId) changes.profile_id = next.modelProfileId
    if (next.thinkingLevel !== settings.thinkingLevel) changes.thinking_level = next.thinkingLevel
    if (Object.keys(changes).length > 0) {
      setError('')
      if (!await conversationConfig.patch(changes)) {
        setError(t('storyPicker.setup.model.saveFailed'))
        return
      }
      // Other opening controls may change while persistence is in progress.
      setSettings((current) => ({ ...current, modelProfileId: next.modelProfileId, thinkingLevel: next.thinkingLevel }))
      return
    }
    setSettings(next)
  }

  const submit = async () => {
    if (creating) return
    setError('')
    if (!conversationConfig.initialized || !settings.modelProfileId) {
      setError(t('storyPicker.setup.model.loadFailed'))
      return
    }
    const validationError = validateDraft(protagonist, opening, loreItems, t)
    if (validationError) {
      setError(validationError)
      return
    }
    setCreating(true)
    try {
      const tellerID = resolveNarrativeStyle(tellers, settings.moduleRefs.narrative_style_id || recentNarrativeStyleID)?.id || DEFAULT_NARRATIVE_STYLE_ID
      const moduleRefs = {
        ...settings.moduleRefs,
        actor_state_id: settings.moduleRefs.actor_state_id || 'default',
        actor_state_disabled: settings.stateSchemaMode === 'generate',
      }
      const protagonistInput = protagonistForSubmit(protagonist)
      const includeProtagonist = !story || !sameProtagonist(protagonist, initialProtagonistRef.current)
      await onCreate({
        title: story?.title_source === 'pending' ? '' : story?.title || '',
        ...(!story && settings.customAgentId ? { custom_agent_id: settings.customAgentId } : {}),
        profile_id: settings.modelProfileId,
        thinking_level: settings.thinkingLevel,
        origin: story?.origin || '',
        ...(includeProtagonist ? { protagonist: protagonistInput } : {}),
        story_teller_id: tellerID,
        planning_template_id: planningTemplateId,
        planning_mode: settings.planningEnabled ? 'enabled' : 'disabled',
        module_refs: moduleRefs,
        reply_target_chars: settings.replyTargetChars,
        choice_count: settings.choiceCount,
        opening: openingForSubmit(opening),
        image_settings: { ...settings.imageSettings, preset_id: settings.imageSettings.preset_id || moduleRefs.image_preset_id || 'game-cg' },
        check_settings: settings.checkSettings,
        state_schema_policy: { mode: settings.stateSchemaMode },
      })
    } catch (reason) {
      console.error('[story-setup] Failed to save and start story', reason)
      setError(reason instanceof Error ? reason.message : t('storyPicker.createFailed'))
      setCreating(false)
    }
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-[var(--nova-surface-2)]">
      <div className="min-h-0 flex-1 overflow-y-auto px-4 pb-6 pt-5 sm:px-7 lg:px-10">
        <section className="mx-auto w-full max-w-5xl" aria-labelledby="new-story-title">
          <header className="mb-4">
            <h2 id="new-story-title" className="text-xl font-semibold tracking-[-0.02em] text-foreground sm:text-2xl">{story ? t('storyPicker.setup.resumeTitle') : t('storyPicker.setup.title')}</h2>
            <p className="mt-1 text-sm text-muted-foreground">{t('storyPicker.setup.description')}</p>
          </header>

          <div className="flex flex-col gap-4">
            <StoryProtagonistSelector projectId={projectId} value={protagonist} loreItems={loreItems} onChange={changeProtagonist} onRequestLoreInit={onRequestLoreInit} />
            <StoryOpeningSelector value={opening} presets={bookOpeningPresets} onChange={setOpening} />

            <Collapsible open={advancedOpen} onOpenChange={setAdvancedOpen} className="overflow-hidden rounded-xl border border-border bg-card">
              <CollapsibleTrigger asChild>
                <button type="button" className="flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-muted/50 sm:px-5" aria-label={t('storyPicker.setup.advanced.title')}>
                  <span className="flex size-8 shrink-0 items-center justify-center rounded-lg border border-border bg-background text-primary"><SlidersHorizontal className="size-4" /></span>
                  <span className="min-w-0 flex-1">
                    <span className="block text-sm font-medium text-foreground">{t('storyPicker.setup.advanced.title')}</span>
                    <span className="mt-0.5 block truncate text-xs text-muted-foreground">{advancedSummary}</span>
                  </span>
                  <ChevronDown className={cn('size-4 shrink-0 text-muted-foreground transition-transform', advancedOpen && 'rotate-180')} />
                </button>
              </CollapsibleTrigger>
              <CollapsibleContent className="border-t border-border bg-muted/20 p-3 sm:p-4">
                <Field className="mb-3 rounded-lg border border-border bg-background p-3 sm:max-w-xl">
                  <FieldLabel htmlFor="story-setup-planning-template">{t('storyPicker.gamePlanning')}</FieldLabel>
                  <Select value={planningTemplateId} onValueChange={setPlanningTemplateId}>
                    <SelectTrigger id="story-setup-planning-template" className="w-full bg-background">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent position="popper">
                      <SelectGroup>
                        {planningTemplates.map((item) => (
                          <SelectItem key={item.id} value={item.id}>
                            {gamePlanningTemplateName(item, t)}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FieldDescription className="text-xs">
                    {t('storyPicker.setup.planningTemplateHint', { template: planningTemplateName })}
                  </FieldDescription>
                </Field>
                <StorySetupAdvanced
                  projectId={projectId}
                  newStory={!story}
                  tellers={tellers}
                  imagePresets={imagePresets}
                  value={settings}
                  onChange={(next) => { void changeSettings(next) }}
                  runtimeConfigLoading={runtimeConfigLoading || conversationConfig.saving}
                  runtimeConfigError={conversationConfig.error}
                  onRuntimeConfigReload={() => void conversationConfig.reload()}
                  onNarrativeStyleChange={(id) => {
                    narrativeStyleSelectionLockedRef.current = true
                    return onNarrativeStyleChange?.(id)
                  }}
                  onOpenPresets={onOpenPresets}
                />
              </CollapsibleContent>
            </Collapsible>
          </div>

          {error ? <div role="alert" className="mt-4 rounded-lg border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">{error}</div> : null}
        </section>
      </div>
      <footer data-testid="story-setup-footer" className="shrink-0 border-t border-border bg-[var(--nova-surface-2)] px-4 py-3 sm:px-7 lg:px-10">
        <div className="mx-auto flex w-full max-w-5xl items-center justify-end gap-2">
          {!story ? <Button type="button" variant="ghost" disabled={creating} onClick={onCancel}>{t('common.cancel')}</Button> : null}
          <Button
            type="button"
            disabled={creating || narrativeStyleLoading || runtimeConfigLoading || conversationConfig.saving || !runtimeConfigReady}
            onClick={() => void submit()}
          >
            {startButtonLoading ? <Spinner /> : <Play data-icon="inline-start" />}
            {startButtonLabel}
          </Button>
        </div>
      </footer>
    </div>
  )
}

function initialSettings(
  story: StorySummary | undefined,
  recentTellerID: string | undefined,
  runtimeConfig: ConversationConfigSnapshot | null,
): StorySetupSettings {
  const moduleRefs = {
    narrative_style_id: 'rhythm',
    event_package_ids: ['default'],
    rule_system_id: 'default',
    actor_state_id: 'default',
    image_preset_id: 'game-cg',
    ...(story?.module_refs || {}),
  }
  if (!story && recentTellerID) moduleRefs.narrative_style_id = recentTellerID
  const imageSettings = normalizeStoryImageSettings(story?.image_settings || { mode: 'manual', interval_turns: 3, preset_id: moduleRefs.image_preset_id || 'game-cg' })
  return {
    customAgentId: '',
    modelProfileId: runtimeConfig ? runtimeConfig.profile_id || 'default' : '',
    thinkingLevel: normalizeThinkingLevel(runtimeConfig?.thinking_level) || 'default',
    planningEnabled: story ? story.planning_mode === 'enabled' : true,
    moduleRefs,
    replyTargetChars: story?.reply_target_chars || DEFAULT_INTERACTIVE_REPLY_TARGET_CHARS,
    choiceCount: story?.choice_count || DEFAULT_INTERACTIVE_CHOICE_COUNT,
    imageSettings,
    checkSettings: normalizeStoryCheckSettings(story?.check_settings),
    stateSchemaMode: story?.state_schema_policy?.mode || 'adapt_template',
  }
}

function validateDraft(protagonist: StoryProtagonist, opening: StoryOpeningConfig, loreItems: LoreItem[], t: ReturnType<typeof useTranslation>['t']): string {
  if (protagonist.mode === 'custom' && !protagonist.name?.trim()) return t('storyPicker.setup.protagonist.custom.nameRequired')
  if (protagonist.mode === 'lore' && !protagonist.source_lore_item_id?.trim()) return t('storyPicker.setup.protagonist.lore.required')
  if (protagonist.mode === 'default' && !loreItems.some((item) => item.enabled && item.type === 'character')) return t('storyPicker.setup.protagonist.lore.emptyRequired')
  if (opening.mode === 'preset' && !opening.preset_text?.trim()) return t('storyPicker.setup.opening.presetRequired')
  return ''
}

function defaultStoryProtagonist(loreItems: LoreItem[]): StoryProtagonist {
  const tagged = loreItems.find((item) => item.enabled && item.type === 'character' && hasLoreProtagonistTag(item.tags))
  if (!tagged) return { mode: 'default' }
  return {
    mode: 'lore',
    name: tagged.name,
    profile: tagged.content || tagged.brief_description,
    source_lore_item_id: tagged.id,
    source_lore_updated_at: tagged.updated_at,
  }
}

function protagonistForSubmit(protagonist: StoryProtagonist): StoryProtagonist {
  if (protagonist.mode === 'custom') return { mode: 'custom', name: protagonist.name?.trim(), profile: protagonist.profile?.trim() }
  if (protagonist.mode === 'lore') return { mode: 'lore', source_lore_item_id: protagonist.source_lore_item_id?.trim() }
  return { mode: 'default' }
}

function openingForSubmit(opening: StoryOpeningConfig): StoryOpeningConfig {
  if (opening.mode === 'preset') return { mode: 'preset', preset_id: opening.preset_id?.trim(), preset_text: truncateStoryOpeningText(opening.preset_text || '') }
  if (opening.mode === 'custom') {
    const customText = truncateStoryOpeningText(opening.custom_text || '')
    return customText ? { mode: 'custom', custom_text: customText } : { mode: 'ai' }
  }
  return { mode: 'ai' }
}

function sameProtagonist(left: StoryProtagonist, right: StoryProtagonist): boolean {
  return JSON.stringify(protagonistForSubmit(left)) === JSON.stringify(protagonistForSubmit(right))
}
