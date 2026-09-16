-- Migración de Telemetría Normalizada: Hosts, Runs, Events y Artifacts

-- 1. Tabla de Hosts (Inventario de Máquinas)
CREATE TABLE IF NOT EXISTS public.backup_hosts (
    host_id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL,
    domain_name TEXT,
    os_name TEXT,
    os_family TEXT,
    os_version TEXT,
    os_build TEXT,
    os_arch TEXT,
    cpu_model TEXT,
    cpu_cores_logical INT,
    cpu_cores_physical INT,
    total_ram_bytes BIGINT,
    mac_addresses JSONB,
    primary_mac TEXT,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_backup_hosts_hostname ON public.backup_hosts (hostname);
CREATE INDEX IF NOT EXISTS idx_backup_hosts_last_seen ON public.backup_hosts (last_seen_at DESC);

-- 2. Tabla de Corridas de Backup (Runs)
CREATE TABLE IF NOT EXISTS public.backup_runs (
    run_id TEXT PRIMARY KEY,
    host_id TEXT NOT NULL REFERENCES public.backup_hosts(host_id) ON DELETE CASCADE,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    duration_ms BIGINT,
    status TEXT NOT NULL,
    trigger_mode TEXT,
    profile_name TEXT,
    database_name TEXT NOT NULL,
    sql_server_instance TEXT,
    sql_auth_mode TEXT,
    primary_ip TEXT,
    local_ips JSONB,
    username TEXT,
    user_domain TEXT,
    is_elevated_admin BOOLEAN,
    process_id INT,
    process_path TEXT,
    agent_version TEXT,
    go_version TEXT,
    free_ram_bytes BIGINT,
    backup_disk_drive TEXT,
    backup_disk_free_bytes BIGINT,
    backup_disk_total_bytes BIGINT,
    system_uptime_seconds BIGINT,
    timezone TEXT,
    error_message TEXT,
    error_stage TEXT,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_backup_runs_host ON public.backup_runs (host_id);
CREATE INDEX IF NOT EXISTS idx_backup_runs_started ON public.backup_runs (started_at DESC);
CREATE INDEX IF NOT EXISTS idx_backup_runs_status ON public.backup_runs (status);
CREATE INDEX IF NOT EXISTS idx_backup_runs_database ON public.backup_runs (database_name);

-- 3. Tabla de Eventos de la Corrida (Granular Lifecycle Events)
CREATE TABLE IF NOT EXISTS public.backup_events (
    event_id TEXT PRIMARY KEY,
    run_id TEXT REFERENCES public.backup_runs(run_id) ON DELETE CASCADE,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT now(),
    event_type TEXT NOT NULL,
    status TEXT NOT NULL,
    backend TEXT,
    duration_ms BIGINT,
    error_message TEXT,
    details JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_backup_events_run_id ON public.backup_events (run_id);
CREATE INDEX IF NOT EXISTS idx_backup_events_timestamp ON public.backup_events (timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_backup_events_event_type ON public.backup_events (event_type);
CREATE INDEX IF NOT EXISTS idx_backup_events_status ON public.backup_events (status);

-- 4. Tabla de Artefactos Producidos (Destinos de Almacenamiento)
CREATE TABLE IF NOT EXISTS public.backup_artifacts (
    artifact_id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES public.backup_runs(run_id) ON DELETE CASCADE,
    backend TEXT NOT NULL CHECK (backend IN ('local', 'cloudflare_r2', 'remote_unc', 'supabase_storage')),
    filename TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    sha256 TEXT NOT NULL,
    is_verified BOOLEAN DEFAULT true,
    storage_path TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_backup_artifacts_run_id ON public.backup_artifacts (run_id);
CREATE INDEX IF NOT EXISTS idx_backup_artifacts_backend ON public.backup_artifacts (backend);

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'backup_artifacts_backend_check'
    ) THEN
        ALTER TABLE public.backup_artifacts DROP CONSTRAINT backup_artifacts_backend_check;
        ALTER TABLE public.backup_artifacts ADD CONSTRAINT backup_artifacts_backend_check 
            CHECK (backend IN ('local', 'cloudflare_r2', 'remote_unc', 'supabase_storage'));
    END IF;
END $$;

-- 5. Vista de Consultas Rápidas (Unificada)
CREATE OR REPLACE VIEW public.v_backup_runs_full AS
SELECT 
    r.run_id,
    r.status,
    r.trigger_mode,
    r.profile_name,
    r.database_name,
    r.sql_server_instance,
    r.started_at,
    r.finished_at,
    r.duration_ms,
    h.hostname,
    h.os_name,
    h.os_build,
    h.os_arch,
    h.cpu_model,
    ROUND(h.total_ram_bytes / (1024.0 * 1024 * 1024), 2) AS total_ram_gb,
    r.primary_ip,
    r.local_ips,
    r.username,
    r.is_elevated_admin,
    r.process_id,
    ROUND(r.free_ram_bytes / (1024.0 * 1024 * 1024), 2) AS free_ram_gb,
    r.backup_disk_drive,
    ROUND(r.backup_disk_free_bytes / (1024.0 * 1024 * 1024), 2) AS disk_free_gb,
    ROUND(r.backup_disk_total_bytes / (1024.0 * 1024 * 1024), 2) AS disk_total_gb,
    r.error_message,
    r.error_stage
FROM public.backup_runs r
JOIN public.backup_hosts h ON r.host_id = h.host_id;

-- 6. Seguridad a nivel de fila (RLS) y Políticas
ALTER TABLE public.backup_hosts ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.backup_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.backup_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.backup_artifacts ENABLE ROW LEVEL SECURITY;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'backup_hosts' AND policyname = 'Allow all backup_hosts') THEN
        CREATE POLICY "Allow all backup_hosts" ON public.backup_hosts FOR ALL USING (true) WITH CHECK (true);
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'backup_runs' AND policyname = 'Allow all backup_runs') THEN
        CREATE POLICY "Allow all backup_runs" ON public.backup_runs FOR ALL USING (true) WITH CHECK (true);
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'backup_events' AND policyname = 'Allow all backup_events') THEN
        CREATE POLICY "Allow all backup_events" ON public.backup_events FOR ALL USING (true) WITH CHECK (true);
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE schemaname = 'public' AND tablename = 'backup_artifacts' AND policyname = 'Allow all backup_artifacts') THEN
        CREATE POLICY "Allow all backup_artifacts" ON public.backup_artifacts FOR ALL USING (true) WITH CHECK (true);
    END IF;
END
$$;
