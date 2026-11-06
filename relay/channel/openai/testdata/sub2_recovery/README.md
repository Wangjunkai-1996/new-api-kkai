# Sub2 recovery protocol captures

These fixtures are the bytes emitted by Sub2's actual `OpenAIRecoveryStreamWriter`,
with its physical-dispatch observer driving automatic attempt changes. They are
replayed through NewAPI's handlers by `recovery_v1_captured_test.go`.

`capture_test.go.txt` is injected into the Sub2 service test package through Go's
overlay feature; the Sub2 checkout is not modified. Regenerate from the NewAPI
checkout with the confirmed local Sub2 checkout path:

```sh
python3 relay/channel/openai/testdata/sub2_recovery/capture.py --sub2 /path/to/sub2api
go test ./relay/channel/openai -run '^TestManagedRecoveryCapturedSub2' -count=1 -timeout=90s
```

`manifest.json` records the producer source and fixture SHA-256 digests. The
capture fails without replacing fixtures if producer sources change while the
test is running. The fixtures contain synthetic test messages only.

Covered contracts: automatic Begin/Abandon at physical dispatch, visible text
regeneration, terminal-only text, all-choice Chat completion, legacy strict
JSON writer validation, tool frames followed by terminal failure, explicit zero
usage, missing usage winner-only estimation, abandoned reasoning-summary closure
without cipher leakage, and final failure after abandonment without another
dispatch, including all eleven attempts and the maximum 45 control frames.
Refusal cases preserve the native
refusal field, complete missing suffixes from done-only/partial-delta upstream
events, and keep separate content parts across regeneration. Replay covers raw
Responses and Chat, both streaming conversion directions, and Responses SSE
converted to buffered Chat JSON. Explicit JSON requests now bypass the managed
writer at the request boundary; strict writer fixtures retain regression coverage
for its internal validation behavior.
