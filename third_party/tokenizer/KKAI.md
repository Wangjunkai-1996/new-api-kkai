# KKAI tokenizer fork

Based on github.com/tiktoken-go/tokenizer v0.8.1. Original upstream license and
attribution remain in LICENSE and the source files.

The only runtime algorithm change is the heap-based mergePairs implementation
in codec/codec.go. It preserves the original BPE merge order, including the
leftmost tie break, while avoiding repeated full scans for long pieces.
TokenKit shares this fork through an explicit local replace in both module
roots, including when GOWORK=off; it never splits arbitrary 1024-byte runs.
