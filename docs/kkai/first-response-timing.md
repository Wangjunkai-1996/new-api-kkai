# First Response Timing

NewAPI requests Sub2API's own first-token measurement on OpenAI streaming
requests with `X-Sub2-TTFT: 1`. This header is set after header overrides;
nonstreaming requests and other provider types do not opt in.

Sub2API returns the winning upstream attempt's logged measurement as an SSE
comment, for example `: sub2-ttft-ms=1180`. It sends the comment only when the
existing response path commits output, before terminal data, without an
independent flush. Failed attempts that can still retry must not send a comment.
Comments inserted within an SSE event use one terminating newline so they do
not end the surrounding event.

NewAPI keeps the first valid nonnegative integer measurement per relay attempt.
Zero is valid; malformed, negative, and overflowing values are ignored.
`ResetAttemptTiming` clears this value before each controller retry. Reading the
comment does not invoke the data handler, increment response counts, or record
the first SSE timestamp.

New consume logs use these fields, all in milliseconds:

| Field | Measurement |
| --- | --- |
| `frt` | Sub2API's measurement when present; otherwise NewAPI's valid HTTP header latency |
| `sub2_ttft_ms` | Original Sub2API measurement, omitted without a valid comment |
| `upstream_header_ms` | NewAPI's elapsed time from request start to upstream HTTP headers |
| `first_sse_ms` | NewAPI's elapsed time from request start to the first data SSE |

Without either a valid Sub2API comment or a valid HTTP header timestamp, `frt`
is omitted, preserving the existing fallback behavior. Historical logs are not
rewritten. This synchronizes the displayed measurement; it does not make model
output arrive sooner or hide NewAPI's raw arrival timings.

## Focused Verification

```bash
go test -race ./relay/common ./relay/helper ./relay/channel ./service -run 'Test(DoRequestNegotiatesSub2TTFTForOpenAIStreams|DoRequestRecordsUpstreamHeaderTime|StreamScannerHandler_Sub2TTFT|GenerateTextOtherInfo|RelayInfoResetAttemptTiming)' -count=1 -timeout=120s
scripts/kkai/check-frt-header-patch.sh
```
