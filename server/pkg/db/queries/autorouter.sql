-- name: CreateAutoRouterDecision :exec
INSERT INTO autorouter_decision (id, runtime_id, input, expires_at)
VALUES ($1, $2, $3, $4);

-- name: DeleteAutoRouterDecision :exec
DELETE FROM autorouter_decision WHERE id = $1;

-- name: DeleteExpiredAutoRouterDecisions :exec
DELETE FROM autorouter_decision WHERE expires_at < now();

-- name: ClaimAutoRouterDecision :one
UPDATE autorouter_decision AS target SET claimed = true, claim_until = now() + interval '3 seconds'
WHERE target.id = (
    SELECT pending.id FROM autorouter_decision AS pending
    WHERE pending.runtime_id = $1 AND (NOT pending.claimed OR pending.claim_until < now())
      AND pending.result IS NULL AND pending.expires_at > now()
    ORDER BY pending.claimed, pending.expires_at LIMIT 1 FOR UPDATE SKIP LOCKED
)
RETURNING target.*;

-- name: GetAutoRouterDecision :one
SELECT * FROM autorouter_decision WHERE id = $1 AND expires_at > now();

-- name: CompleteAutoRouterDecision :execrows
UPDATE autorouter_decision SET result = $3
WHERE id = $1 AND runtime_id = $2 AND claimed AND result IS NULL AND expires_at > now();

-- name: RouteEmptyChatSession :one
UPDATE chat_session AS target SET agent_id = @agent_id, updated_at = now()
WHERE target.id = @id AND target.agent_id = @router_id
AND NOT EXISTS (SELECT 1 FROM chat_message WHERE chat_session_id = @id)
AND NOT EXISTS (SELECT 1 FROM agent_task_queue WHERE chat_session_id = @id)
RETURNING target.*;
