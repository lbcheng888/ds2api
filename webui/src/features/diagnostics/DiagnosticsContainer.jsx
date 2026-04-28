import { useCallback, useEffect, useMemo, useState } from 'react'
import { Activity, AlertTriangle, Clipboard, Database, RefreshCw, ShieldAlert, Terminal } from 'lucide-react'

import { useI18n } from '../../i18n'

export default function DiagnosticsContainer({ authFetch, onMessage }) {
    const { t } = useI18n()
    const [data, setData] = useState(null)
    const [loading, setLoading] = useState(false)
    const [error, setError] = useState('')

    const loadDiagnostics = useCallback(async () => {
        setLoading(true)
        setError('')
        try {
            const res = await authFetch('/admin/dev/diagnostics?limit=20')
            const payload = await res.json()
            if (!res.ok) {
                throw new Error(payload.detail || t('diagnostics.loadFailed'))
            }
            setData(payload)
        } catch (err) {
            const message = err?.message || t('diagnostics.loadFailed')
            setError(message)
            onMessage?.('error', message)
        } finally {
            setLoading(false)
        }
    }, [authFetch, onMessage, t])

    useEffect(() => {
        loadDiagnostics()
    }, [loadDiagnostics])

    const failureSamples = Array.isArray(data?.failure_samples) ? data.failure_samples : []
    const captureChains = Array.isArray(data?.capture_chains) ? data.capture_chains : []
    const accountHealth = Array.isArray(data?.queue_status?.account_health) ? data.queue_status.account_health : []
    const coolingAccounts = accountHealth.filter(item => item?.status === 'cooldown')
    const accountFailures = accountHealth.reduce((sum, item) => sum + Number(item?.failure_count || 0), 0)
    const runtimeProfile = data?.runtime_profile && typeof data.runtime_profile === 'object' ? data.runtime_profile : {}
    const harnessMetrics = data?.harness_metrics && typeof data.harness_metrics === 'object' ? data.harness_metrics : {}
    const failureSummary = data?.failure_summary && typeof data.failure_summary === 'object' ? data.failure_summary : {}
    const latestFailure = failureSummary.latest && typeof failureSummary.latest === 'object' ? failureSummary.latest : null
    const repairRows = Object.entries(harnessMetrics.repairs || {}).sort((a, b) => Number(b[1]) - Number(a[1]))
    const streamRows = Object.entries(harnessMetrics.streams || {}).sort((a, b) => Number(b[1]) - Number(a[1]))
    const failureRows = Object.entries(harnessMetrics.failures || {}).sort((a, b) => Number(b[1]) - Number(a[1]))
    const categoryRows = Object.entries(failureSummary.by_category || {}).sort((a, b) => Number(b[1]) - Number(a[1]))
    const errorRows = Object.entries(failureSummary.by_error_code || {}).sort((a, b) => Number(b[1]) - Number(a[1]))

    const cards = useMemo(() => [
        {
            label: t('diagnostics.failureSamples'),
            value: failureSummary.total ?? failureSamples.length,
            icon: AlertTriangle,
        },
        {
            label: t('diagnostics.captureChains'),
            value: data?.capture_count ?? captureChains.length,
            icon: Database,
        },
        {
            label: t('diagnostics.coolingAccounts'),
            value: coolingAccounts.length,
            icon: ShieldAlert,
        },
        {
            label: t('diagnostics.devCapture'),
            value: data?.dev_capture_on ? t('diagnostics.enabled') : t('diagnostics.disabled'),
            icon: Activity,
        },
    ], [captureChains.length, coolingAccounts.length, data?.capture_count, data?.dev_capture_on, failureSamples.length, failureSummary.total, t])

    const copyText = async (text) => {
        try {
            await navigator.clipboard.writeText(String(text || ''))
            onMessage?.('success', t('diagnostics.copied'))
        } catch (_err) {
            onMessage?.('error', t('messages.copyFailed'))
        }
    }

    return (
        <div className="space-y-6">
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div className="space-y-1">
                    <p className="text-sm text-muted-foreground">{t('diagnostics.description')}</p>
                    {data?.raw_sample_root && (
                        <p className="text-xs font-mono text-muted-foreground break-all">{data.raw_sample_root}</p>
                    )}
                </div>
                <button
                    onClick={loadDiagnostics}
                    disabled={loading}
                    className="inline-flex h-10 items-center justify-center gap-2 rounded-lg border border-border bg-card px-4 text-sm font-medium text-foreground hover:bg-secondary disabled:opacity-60"
                >
                    <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
                    {t('diagnostics.refresh')}
                </button>
            </div>

            {error && (
                <div className="rounded-lg border border-destructive/20 bg-destructive/10 p-4 text-sm text-destructive">
                    {error}
                </div>
            )}

            <div className="grid grid-cols-1 gap-4 md:grid-cols-4">
                {cards.map(card => {
                    const Icon = card.icon
                    return (
                        <div key={card.label} className="rounded-xl border border-border bg-card p-4 shadow-sm">
                            <div className="flex items-center justify-between">
                                <p className="text-xs font-semibold uppercase tracking-widest text-muted-foreground">{card.label}</p>
                                <Icon className="h-4 w-4 text-muted-foreground" />
                            </div>
                            <div className="mt-3 text-2xl font-bold text-foreground">{card.value}</div>
                        </div>
                    )
                })}
            </div>

            <section className="rounded-xl border border-border bg-card p-4 shadow-sm">
                <div className="mb-3 flex items-center justify-between">
                    <div>
                        <h3 className="font-semibold">{t('diagnostics.runtimeProfile')}</h3>
                        <p className="text-sm text-muted-foreground">{t('diagnostics.runtimeProfileDesc')}</p>
                    </div>
                    <Activity className="h-5 w-5 text-primary" />
                </div>
                <div className="grid gap-2 text-xs text-muted-foreground md:grid-cols-3">
                    <div>{t('diagnostics.reasoningTimeout')}: {runtimeProfile.reasoning_only_timeout_seconds || '-'}</div>
                    <div>{t('diagnostics.streamTimeout')}: {runtimeProfile.stream_max_duration_seconds || '-'}</div>
                    <div>{t('diagnostics.defaultReasoning')}: {runtimeProfile.default_reasoning_effort || '-'}</div>
                    <div>{t('diagnostics.metaAgents')}: {String(Boolean(runtimeProfile.allow_meta_agent_tools))}</div>
                    <div>{t('diagnostics.historySplit')}: {String(Boolean(runtimeProfile.history_split_enabled))}</div>
                    <div>{t('diagnostics.bufferLimit')}: {runtimeProfile.buffered_tool_content_max_bytes || '-'}</div>
                </div>
            </section>

            <section className="rounded-xl border border-border bg-card p-4 shadow-sm">
                <div className="mb-3 flex items-center justify-between">
                    <div>
                        <h3 className="font-semibold">{t('diagnostics.harnessMetrics')}</h3>
                        <p className="text-sm text-muted-foreground">{t('diagnostics.harnessMetricsDesc')}</p>
                    </div>
                    <Activity className="h-5 w-5 text-primary" />
                </div>
                <div className="grid gap-4 lg:grid-cols-3">
                    {[
                        [t('diagnostics.repairs'), repairRows],
                        [t('diagnostics.streamRepairs'), streamRows],
                        [t('diagnostics.failureDecisions'), failureRows],
                    ].map(([label, rows]) => (
                        <div key={label} className="rounded-lg border border-border bg-background p-3">
                            <div className="mb-2 text-xs font-semibold text-muted-foreground">{label}</div>
                            <div className="space-y-2 text-xs">
                                {rows.length === 0 ? (
                                    <div className="text-muted-foreground">-</div>
                                ) : rows.map(([key, value]) => (
                                    <div key={key} className="flex items-center justify-between gap-3">
                                        <span className="break-all text-muted-foreground">{key}</span>
                                        <span className="font-semibold text-foreground">{value}</span>
                                    </div>
                                ))}
                            </div>
                        </div>
                    ))}
                </div>
            </section>

            <section className="rounded-xl border border-border bg-card p-4 shadow-sm">
                <div className="mb-3 flex items-center justify-between">
                    <div>
                        <h3 className="font-semibold">{t('diagnostics.failureSummary')}</h3>
                        <p className="text-sm text-muted-foreground">{t('diagnostics.failureSummaryDesc')}</p>
                    </div>
                    <AlertTriangle className="h-5 w-5 text-amber-500" />
                </div>
                <div className="grid gap-4 lg:grid-cols-3">
                    <div className="rounded-lg border border-border bg-background p-3">
                        <div className="mb-2 text-xs font-semibold text-muted-foreground">{t('diagnostics.byCategory')}</div>
                        <div className="space-y-2 text-xs">
                            {categoryRows.length === 0 ? (
                                <div className="text-muted-foreground">-</div>
                            ) : categoryRows.map(([key, value]) => (
                                <div key={key} className="flex items-center justify-between gap-3">
                                    <span className="break-all text-muted-foreground">{key}</span>
                                    <span className="font-semibold text-foreground">{value}</span>
                                </div>
                            ))}
                        </div>
                    </div>
                    <div className="rounded-lg border border-border bg-background p-3">
                        <div className="mb-2 text-xs font-semibold text-muted-foreground">{t('diagnostics.byErrorCode')}</div>
                        <div className="space-y-2 text-xs">
                            {errorRows.length === 0 ? (
                                <div className="text-muted-foreground">-</div>
                            ) : errorRows.map(([key, value]) => (
                                <div key={key} className="flex items-center justify-between gap-3">
                                    <span className="break-all text-muted-foreground">{key}</span>
                                    <span className="font-semibold text-foreground">{value}</span>
                                </div>
                            ))}
                        </div>
                    </div>
                    <div className="rounded-lg border border-border bg-background p-3">
                        <div className="mb-2 text-xs font-semibold text-muted-foreground">{t('diagnostics.latestFailure')}</div>
                        {latestFailure ? (
                            <div className="space-y-2 text-xs text-muted-foreground">
                                <div className="break-all font-mono text-foreground">{latestFailure.sample_id}</div>
                                <div>{latestFailure.category || '-'} · {latestFailure.error_code || '-'}</div>
                                <button
                                    onClick={() => copyText(latestFailure.replay_command)}
                                    className="inline-flex h-8 items-center justify-center gap-2 rounded-md border border-border px-3 text-xs font-medium text-muted-foreground hover:bg-secondary hover:text-foreground"
                                >
                                    <Clipboard className="h-3.5 w-3.5" />
                                    {t('diagnostics.copyReplay')}
                                </button>
                            </div>
                        ) : (
                            <div className="text-xs text-muted-foreground">-</div>
                        )}
                    </div>
                </div>
            </section>

            <section className="rounded-xl border border-border bg-card shadow-sm">
                <div className="flex items-center justify-between border-b border-border px-5 py-4">
                    <div>
                        <h3 className="font-semibold">{t('diagnostics.failureSamples')}</h3>
                        <p className="text-sm text-muted-foreground">{t('diagnostics.failureSamplesDesc')}</p>
                    </div>
                    <AlertTriangle className="h-5 w-5 text-amber-500" />
                </div>
                <div className="divide-y divide-border">
                    {failureSamples.length === 0 ? (
                        <div className="px-5 py-8 text-sm text-muted-foreground">{t('diagnostics.noFailureSamples')}</div>
                    ) : failureSamples.map(sample => (
                        <div key={sample.sample_id} className="space-y-3 px-5 py-4">
                            <div className="flex flex-col gap-2 lg:flex-row lg:items-start lg:justify-between">
                                <div className="min-w-0">
                                    <div className="flex flex-wrap items-center gap-2">
                                        <span className="font-mono text-sm font-semibold text-foreground break-all">{sample.sample_id}</span>
                                        {sample.error_code && (
                                            <span className="rounded-full border border-amber-500/20 bg-amber-500/10 px-2 py-0.5 text-[11px] font-semibold text-amber-500">
                                                {sample.error_code}
                                            </span>
                                        )}
                                        {sample.category && (
                                            <span className="rounded-full border border-primary/20 bg-primary/10 px-2 py-0.5 text-[11px] font-semibold text-primary">
                                                {sample.category}
                                            </span>
                                        )}
                                    </div>
                                    <p className="mt-1 text-xs text-muted-foreground">{sample.captured_at_utc || '-'}</p>
                                </div>
                                <button
                                    onClick={() => copyText(sample.replay_command)}
                                    className="inline-flex h-8 shrink-0 items-center justify-center gap-2 rounded-md border border-border px-3 text-xs font-medium text-muted-foreground hover:bg-secondary hover:text-foreground"
                                >
                                    <Clipboard className="h-3.5 w-3.5" />
                                    {t('diagnostics.copyReplay')}
                                </button>
                            </div>
                            <div className="rounded-lg border border-border bg-background p-3">
                                <div className="mb-2 flex items-center gap-2 text-xs font-semibold text-muted-foreground">
                                    <Terminal className="h-3.5 w-3.5" />
                                    {t('diagnostics.replayCommand')}
                                </div>
                                <code className="block break-all text-xs text-foreground">{sample.replay_command}</code>
                            </div>
                            {sample.analysis && (
                                <div className="grid gap-2 rounded-lg border border-border bg-background p-3 text-xs text-muted-foreground md:grid-cols-3">
                                    <div>{t('diagnostics.events')}: {sample.analysis.event_count || 0}</div>
                                    <div>{t('diagnostics.visibleChars')}: {sample.analysis.visible_chars || 0}</div>
                                    <div>{t('diagnostics.reasoningChars')}: {sample.analysis.reasoning_chars || 0}</div>
                                </div>
                            )}
                            <div className="grid gap-2 text-xs text-muted-foreground md:grid-cols-2">
                                <div className="break-all">{t('diagnostics.metaPath')}: {sample.meta_path}</div>
                                <div className="break-all">{t('diagnostics.upstreamPath')}: {sample.upstream_path}</div>
                            </div>
                        </div>
                    ))}
                </div>
            </section>

            <section className="rounded-xl border border-border bg-card shadow-sm">
                <div className="flex items-center justify-between border-b border-border px-5 py-4">
                    <div>
                        <h3 className="font-semibold">{t('diagnostics.accountHealth')}</h3>
                        <p className="text-sm text-muted-foreground">{t('diagnostics.accountHealthDesc', { failures: accountFailures })}</p>
                    </div>
                    <ShieldAlert className="h-5 w-5 text-amber-500" />
                </div>
                <div className="divide-y divide-border">
                    {accountHealth.length === 0 ? (
                        <div className="px-5 py-8 text-sm text-muted-foreground">{t('diagnostics.noAccountHealth')}</div>
                    ) : accountHealth.map(item => (
                        <div key={item.account_id} className="grid gap-3 px-5 py-4 lg:grid-cols-[minmax(0,1fr)_auto]">
                            <div className="min-w-0">
                                <div className="flex flex-wrap items-center gap-2">
                                    <span className="break-all font-mono text-sm font-semibold text-foreground">{item.account_id}</span>
                                    <span className={`rounded-full border px-2 py-0.5 text-[11px] font-semibold ${item.status === 'cooldown' ? 'border-amber-500/20 bg-amber-500/10 text-amber-500' : 'border-emerald-500/20 bg-emerald-500/10 text-emerald-500'}`}>
                                        {item.status}
                                    </span>
                                </div>
                                {item.cooldown_remaining_seconds > 0 && (
                                    <p className="mt-1 text-xs text-muted-foreground">
                                        {t('accountManager.cooldownRemaining').replace('{seconds}', String(item.cooldown_remaining_seconds))}
                                    </p>
                                )}
                            </div>
                            <div className="grid grid-cols-3 gap-3 text-xs text-muted-foreground">
                                <div>{t('diagnostics.successes')}: {item.success_count || 0}</div>
                                <div>{t('diagnostics.failures')}: {item.failure_count || 0}</div>
                                <div>{t('diagnostics.consecutiveFailures')}: {item.consecutive_failures || 0}</div>
                            </div>
                        </div>
                    ))}
                </div>
            </section>

            <section className="rounded-xl border border-border bg-card shadow-sm">
                <div className="flex items-center justify-between border-b border-border px-5 py-4">
                    <div>
                        <h3 className="font-semibold">{t('diagnostics.recentCaptures')}</h3>
                        <p className="text-sm text-muted-foreground">{t('diagnostics.recentCapturesDesc')}</p>
                    </div>
                    <Database className="h-5 w-5 text-primary" />
                </div>
                <div className="divide-y divide-border">
                    {captureChains.length === 0 ? (
                        <div className="px-5 py-8 text-sm text-muted-foreground">{t('diagnostics.noCaptures')}</div>
                    ) : captureChains.map(chain => (
                        <div key={chain.chain_key} className="grid gap-3 px-5 py-4 lg:grid-cols-[minmax(0,1fr)_auto]">
                            <div className="min-w-0">
                                <div className="font-mono text-sm font-semibold text-foreground break-all">{chain.chain_key}</div>
                                <p className="mt-1 text-xs text-muted-foreground">
                                    {chain.latest_label || chain.initial_label || '-'} · {chain.round_count || 0} {t('diagnostics.rounds')}
                                </p>
                                <p className="mt-2 break-words text-xs text-muted-foreground">{chain.response_preview || '-'}</p>
                            </div>
                            <button
                                onClick={() => copyText(JSON.stringify({ chain_key: chain.chain_key }))}
                                className="inline-flex h-8 items-center justify-center gap-2 rounded-md border border-border px-3 text-xs font-medium text-muted-foreground hover:bg-secondary hover:text-foreground"
                            >
                                <Clipboard className="h-3.5 w-3.5" />
                                {t('actions.copy')}
                            </button>
                        </div>
                    ))}
                </div>
            </section>
        </div>
    )
}
