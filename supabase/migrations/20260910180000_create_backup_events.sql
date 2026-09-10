-- Migración inicial: Tabla centralizada de eventos operativos para el Backup Agent
CREATE TABLE IF NOT EXISTS public.backup_events (
    event_id TEXT PRIMARY KEY,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT now(),
    event_type TEXT NOT NULL,
    status TEXT NOT NULL,
    backend TEXT,
    hostname TEXT,
    database_name TEXT,
    filename TEXT,
    size_bytes BIGINT,
    duration_ms BIGINT,
    error_message TEXT,
    agent_version TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Índices para optimizar consultas de auditoría y monitoreo
CREATE INDEX IF NOT EXISTS idx_backup_events_timestamp ON public.backup_events (timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_backup_events_event_type ON public.backup_events (event_type);
CREATE INDEX IF NOT EXISTS idx_backup_events_status ON public.backup_events (status);

-- Seguridad a nivel de fila (RLS)
ALTER TABLE public.backup_events ENABLE ROW LEVEL SECURITY;

-- Políticas de acceso para servicio autenticado
CREATE POLICY "Allow insert backup_events" ON public.backup_events
    FOR INSERT WITH CHECK (true);

CREATE POLICY "Allow select backup_events" ON public.backup_events
    FOR SELECT USING (true);
