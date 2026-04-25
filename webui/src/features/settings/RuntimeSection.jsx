export default function RuntimeSection({ t, form, setForm }) {
    return (
        <div className="bg-card border border-border rounded-xl p-5 space-y-4">
            <h3 className="font-semibold">{t('settings.runtimeTitle')}</h3>
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.accountMaxInflight')}</span>
                    <input
                        type="number"
                        min={1}
                        value={form.runtime.account_max_inflight}
                        onChange={(e) => setForm((prev) => ({
                            ...prev,
                            runtime: { ...prev.runtime, account_max_inflight: Number(e.target.value || 1) },
                        }))}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2"
                    />
                </label>
                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.accountMaxQueue')}</span>
                    <input
                        type="number"
                        min={1}
                        value={form.runtime.account_max_queue}
                        onChange={(e) => setForm((prev) => ({
                            ...prev,
                            runtime: { ...prev.runtime, account_max_queue: Number(e.target.value || 1) },
                        }))}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2"
                    />
                </label>
                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.globalMaxInflight')}</span>
                    <input
                        type="number"
                        min={1}
                        value={form.runtime.global_max_inflight}
                        onChange={(e) => setForm((prev) => ({
                            ...prev,
                            runtime: { ...prev.runtime, global_max_inflight: Number(e.target.value || 1) },
                        }))}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2"
                    />
                </label>
                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.tokenRefreshIntervalHours')}</span>
                    <input
                        type="number"
                        min={1}
                        max={720}
                        step={1}
                        value={form.runtime.token_refresh_interval_hours}
                        onChange={(e) => setForm((prev) => ({
                            ...prev,
                            runtime: { ...prev.runtime, token_refresh_interval_hours: Number(e.target.value || 1) },
                        }))}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2"
                    />
                </label>
                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.accountFailureCooldownSeconds')}</span>
                    <input
                        type="number"
                        min={1}
                        max={3600}
                        step={1}
                        value={form.runtime.account_failure_cooldown_seconds}
                        onChange={(e) => setForm((prev) => ({
                            ...prev,
                            runtime: { ...prev.runtime, account_failure_cooldown_seconds: Number(e.target.value || 1) },
                        }))}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2"
                    />
                </label>
                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.streamMaxDurationSeconds')}</span>
                    <input
                        type="number"
                        min={30}
                        max={3600}
                        step={1}
                        value={form.runtime.stream_max_duration_seconds}
                        onChange={(e) => setForm((prev) => ({
                            ...prev,
                            runtime: { ...prev.runtime, stream_max_duration_seconds: Number(e.target.value || 30) },
                        }))}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2"
                    />
                </label>
                <label className="text-sm space-y-2">
                    <span className="text-muted-foreground">{t('settings.bufferedToolContentMaxBytes')}</span>
                    <input
                        type="number"
                        min={32768}
                        max={10485760}
                        step={1024}
                        value={form.runtime.buffered_tool_content_max_bytes}
                        onChange={(e) => setForm((prev) => ({
                            ...prev,
                            runtime: { ...prev.runtime, buffered_tool_content_max_bytes: Number(e.target.value || 32768) },
                        }))}
                        className="w-full bg-background border border-border rounded-lg px-3 py-2"
                    />
                </label>
            </div>
        </div>
    )
}
