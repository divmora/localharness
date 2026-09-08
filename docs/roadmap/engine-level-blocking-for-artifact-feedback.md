# Future Roadmap

Items tracked here are planned but not yet implemented. Each item includes
the motivation, triggering conditions, and a rough sketch of the approach.

---

## Engine-Level Blocking for Artifact Feedback

**Status**: Future (not yet implemented)
**Priority**: Medium — implement when prompt-only approach proves insufficient
**Depends on**: `OnArtifactFeedbackHook`, `RequestFeedback` field (already shipped)

### Current Behavior (Non-Blocking)

When the agent creates an artifact with `request_feedback = true`:
1. The engine saves the `.metadata.json` sidecar
2. `OnArtifactFeedbackHook` fires (observability only — does not pause)
3. The system prompt tells the agent to stop calling tools and wait
4. The user responds with a regular chat message in the next turn

This relies entirely on the LLM obeying the system prompt instruction to stop.

### Why Engine-Level Blocking May Be Needed

| Condition | Risk |
|:---|:---|
| **Agent ignores the stop instruction** | Weaker models or high-temperature settings may skip the "wait for approval" instruction and proceed to execute the plan immediately, making unwanted code changes |
| **Autonomous / unattended runs** | In `/goal` mode or CI pipelines, there is no human watching. Without an engine-level gate, the agent could execute a flawed plan with no review checkpoint |
| **Cost control** | A plan that the user would reject could trigger expensive multi-file refactors. Blocking prevents wasted tokens and compute |
| **Safety-critical workspaces** | Production codebases or infrastructure-as-code repos where an unapproved change could cause outages. A hard gate is the only safe option |
| **SDK UX expectations** | SDKs building rich UIs (approve/reject buttons, diff previews) need the engine to pause so they can present the plan before execution continues |

### Proposed Approach

Model it after the existing `ask_question` / permission request pattern:

1. **Engine detects** `request_feedback = true` on a `write_to_file` or `replace_file_content` call targeting the brain directory
2. **Engine emits** `STATE_WAITING` step with a new `ActionArtifactReview` action containing the artifact path, type, and summary
3. **Engine blocks** the turn (same `chan`-based blocking as `requestPermission`)
4. **SDK responds** with an `ArtifactReviewResponse` (approved / rejected / revised instructions)
5. **Engine resumes** — on approval, the turn continues; on rejection, the response is fed back to the LLM as a tool result

### Proto Sketch

```proto
message ActionArtifactReview {
  string request_id = 1;
  string artifact_path = 2;
  string artifact_type = 3;     // "implementation_plan", "walkthrough", etc.
  string summary = 4;

  // Response (set by SDK)
  bool approved = 10;
  string user_feedback = 11;    // Optional revision instructions
}
```

### Gating / Opt-In

This should be **opt-in** via `HarnessConfig`, not default behavior:

```proto
message HarnessConfig {
  // ...
  bool block_on_artifact_feedback = 30;  // Default: false (non-blocking)
}
```

This keeps the current non-blocking flow as default while giving SDKs
the option to enforce hard review gates when needed.

### Migration Path

1. **Phase 1 (current)**: Non-blocking prompt-only + `OnArtifactFeedbackHook`
2. **Phase 2**: Add `block_on_artifact_feedback` config + `ActionArtifactReview` proto
3. **Phase 3**: SDKs can enable blocking for autonomous/CI workflows
