CREATE SEQUENCE IF NOT EXISTS agent_lifecycle_revision;

CREATE TABLE IF NOT EXISTS agent_lifecycle_events (
    revision bigint PRIMARY KEY DEFAULT nextval('agent_lifecycle_revision'),
    machine_id text NOT NULL,
    session_id text NOT NULL DEFAULT '',
    incarnation text NOT NULL DEFAULT '',
    kind text NOT NULL,
    occurred_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    source text NOT NULL,
    source_ref text NOT NULL,
    turn_id text NOT NULL DEFAULT '',
    outcome text NOT NULL DEFAULT '',
    duration_ms bigint NOT NULL DEFAULT 0,
    confidence text NOT NULL,
    space_ref text NOT NULL DEFAULT '',
    todo_id text NOT NULL DEFAULT '',
    UNIQUE (machine_id, session_id, incarnation, kind, source, source_ref)
);

CREATE INDEX IF NOT EXISTS agent_lifecycle_session
    ON agent_lifecycle_events (machine_id, session_id, revision DESC);
CREATE INDEX IF NOT EXISTS agent_lifecycle_todo
    ON agent_lifecycle_events (todo_id, revision DESC) WHERE todo_id <> '';
CREATE INDEX IF NOT EXISTS agent_lifecycle_retention
    ON agent_lifecycle_events (occurred_at);

CREATE TABLE IF NOT EXISTS agent_lifecycle_sources (
    machine_id text PRIMARY KEY,
    stream text NOT NULL,
    ack bigint NOT NULL CHECK (ack >= 0)
);

-- A revision may become visible only after every smaller revision has committed or rolled back.
CREATE OR REPLACE FUNCTION agent_lifecycle_lock() RETURNS void LANGUAGE sql AS $$
    SELECT pg_advisory_xact_lock(hashtext('agent_lifecycle_events'));
$$;

CREATE OR REPLACE FUNCTION agent_lifecycle_record_interaction() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    event_kind text;
    event_at timestamptz;
    event_outcome text := '';
BEGIN
    IF TG_OP = 'INSERT' THEN
        event_kind := 'wait.opened';
        event_at := NEW.created_at;
    ELSIF OLD.status = 'pending' AND NEW.status <> 'pending' THEN
        event_kind := 'wait.resolved';
        event_at := COALESCE(NEW.resolved_at, clock_timestamp());
        event_outcome := CASE WHEN NEW.status IN ('answered', 'resolved', 'expired', 'cancelled', 'dismissed', 'superseded', 'denied')
            THEN NEW.status ELSE 'closed' END;
    ELSE
        RETURN NULL;
    END IF;
    IF NEW.machine_id = '' OR NEW.session_id = '' THEN
        RETURN NULL;
    END IF;

    PERFORM agent_lifecycle_lock();
    INSERT INTO agent_lifecycle_events (machine_id, session_id, kind, occurred_at, source, source_ref, outcome, confidence, todo_id)
    VALUES (NEW.machine_id, NEW.session_id, event_kind, event_at, 'interaction', NEW.id, event_outcome, 'confirmed',
        coalesce((SELECT todo_id FROM todo_agents WHERE machine_id = NEW.machine_id AND session_id = NEW.session_id
            AND linked_at <= NEW.created_at AND (released_at IS NULL OR released_at > NEW.created_at)
            ORDER BY linked_at DESC LIMIT 1), ''))
    ON CONFLICT (machine_id, session_id, incarnation, kind, source, source_ref) DO NOTHING;
    RETURN NULL;
END;
$$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'agent_lifecycle_interaction'
                   AND tgrelid = 'interactions'::regclass) THEN
        CREATE CONSTRAINT TRIGGER agent_lifecycle_interaction
            AFTER INSERT OR UPDATE ON interactions DEFERRABLE INITIALLY DEFERRED
            FOR EACH ROW EXECUTE FUNCTION agent_lifecycle_record_interaction();
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION agent_lifecycle_record_child() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.source NOT IN ('spawned', 'leader', 'worker', 'review') OR NEW.session_id = '' OR NEW.start_error <> ''
       OR NEW.released_at IS NOT NULL OR NEW.dismissed_at IS NOT NULL THEN
        RETURN NULL;
    END IF;
    IF TG_OP = 'UPDATE' AND OLD.source = NEW.source AND OLD.linked_at = NEW.linked_at
       AND OLD.released_at IS NULL AND OLD.dismissed_at IS NULL THEN
        RETURN NULL;
    END IF;

    PERFORM agent_lifecycle_lock();
    INSERT INTO agent_lifecycle_events (machine_id, session_id, kind, occurred_at, source, source_ref, confidence, todo_id)
    VALUES (NEW.machine_id, NEW.session_id, 'child.linked', NEW.linked_at, 'todo_membership',
        md5(jsonb_build_array(NEW.todo_id, NEW.linked_at)::text), 'confirmed', NEW.todo_id)
    ON CONFLICT (machine_id, session_id, incarnation, kind, source, source_ref) DO NOTHING;
    RETURN NULL;
END;
$$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'agent_lifecycle_child'
                   AND tgrelid = 'todo_agents'::regclass) THEN
        CREATE CONSTRAINT TRIGGER agent_lifecycle_child
            AFTER INSERT OR UPDATE ON todo_agents DEFERRABLE INITIALLY DEFERRED
            FOR EACH ROW EXECUTE FUNCTION agent_lifecycle_record_child();
    END IF;
END;
$$;
