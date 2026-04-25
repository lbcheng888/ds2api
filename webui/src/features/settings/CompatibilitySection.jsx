import { ShieldAlert } from 'lucide-react'

export default function CompatibilitySection({ t, form, setForm }) {
    const toggleCompat = (key, defaultValue) => {
        setForm((prev) => ({
            ...prev,
            compat: { ...prev.compat, [key]: !Boolean(prev.compat?.[key] ?? defaultValue) },
        }))
    }

    const switchClass = (enabled) => `relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors ${
        enabled ? 'bg-primary' : 'bg-muted'
    }`

    return (
        <div className="bg-card border border-border rounded-xl p-5 space-y-4">
            <div className="flex items-center gap-2">
                <ShieldAlert className="w-4 h-4 text-muted-foreground" />
                <h3 className="font-semibold">{t('settings.compatibilityTitle')}</h3>
            </div>
            <p className="text-sm text-muted-foreground">{t('settings.compatibilityDesc')}</p>
            <div className="flex items-center justify-between gap-4">
                <label className="text-sm font-medium">{t('settings.stripReferenceMarkers')}</label>
                <button
                    type="button"
                    role="switch"
                    aria-checked={form.compat?.strip_reference_markers ?? true}
                    onClick={() => toggleCompat('strip_reference_markers', true)}
                    className={switchClass(form.compat?.strip_reference_markers ?? true)}
                >
                    <span
                        className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                            form.compat?.strip_reference_markers ?? true ? 'translate-x-6' : 'translate-x-1'
                        }`}
                    />
                </button>
            </div>
            <div className="flex items-center justify-between gap-4">
                <div className="space-y-1">
                    <label className="text-sm font-medium">{t('settings.allowMetaAgentTools')}</label>
                    <p className="text-xs text-muted-foreground">{t('settings.allowMetaAgentToolsDesc')}</p>
                </div>
                <button
                    type="button"
                    role="switch"
                    aria-checked={Boolean(form.compat?.allow_meta_agent_tools ?? false)}
                    onClick={() => toggleCompat('allow_meta_agent_tools', false)}
                    className={switchClass(Boolean(form.compat?.allow_meta_agent_tools ?? false))}
                >
                    <span
                        className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                            form.compat?.allow_meta_agent_tools ?? false ? 'translate-x-6' : 'translate-x-1'
                        }`}
                    />
                </button>
            </div>
        </div>
    )
}
