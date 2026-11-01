package codec

import (
	"container/heap"
	"fmt"
	"math"

	"github.com/dlclark/regexp2"
)

type Codec struct {
	vocabulary        vocab
	reverseVocabulary reverse
	specialTokens     map[string]uint
	splitRegexp       *regexp2.Regexp
	name              string
}

func (c *Codec) GetName() string {
	return c.name
}

// Count returns the number of tokens in the input string.
func (c *Codec) Count(input string) (int, error) {
	var count int

	err := c.tokenize(input, func(_ uint, _ string) {
		count++
	})

	return count, err
}

// Encode returns the token IDs and tokens for the input string.
func (c *Codec) Encode(input string) ([]uint, []string, error) {

	var ids []uint
	var tokens []string

	err := c.tokenize(input, func(id uint, token string) {
		ids = append(ids, id)
		tokens = append(tokens, token)
	})

	return ids, tokens, err
}

func (c *Codec) tokenize(input string, yield func(uint, string)) error {
	match, err := c.splitRegexp.FindStringMatch(input)
	if err != nil {
		return fmt.Errorf("error matching: %v", err)
	}
	for match != nil {
		piece := match.String()
		if id, ok := c.vocabulary[piece]; ok {
			yield(id, piece)
		} else {
			parts := c.mergePairs(piece)

			for i := range len(parts) - 1 {
				token := piece[parts[i].offset:parts[i+1].offset]
				yield(c.vocabulary[token], token)
			}
		}
		match, err = c.splitRegexp.FindNextMatch(match)
		if err != nil {
			return fmt.Errorf("error matching: %v", err)
		}
	}

	return nil
}

func (c *Codec) Decode(tokens []uint) (string, error) {
	if c.reverseVocabulary == nil {
		c.reverseVocabulary = make(map[uint]string)
		for k, v := range c.vocabulary {
			c.reverseVocabulary[v] = k
		}
	}

	var out string
	for _, t := range tokens {
		piece, ok := c.reverseVocabulary[t]
		if !ok {
			return "", fmt.Errorf("invalid token: %d", t)
		}
		out += piece
	}
	return out, nil
}

type part struct {
	offset int
	rank   uint
}

type mergeNode struct {
	prev, next int
	version    uint32
}

type mergeCandidate struct {
	rank    uint
	index   int
	version uint32
}

type mergeHeap []mergeCandidate

func (h mergeHeap) Len() int           { return len(h) }
func (h mergeHeap) Less(i, j int) bool { return h[i].rank < h[j].rank || h[i].rank == h[j].rank && h[i].index < h[j].index }
func (h mergeHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *mergeHeap) Push(value any)    { *h = append(*h, value.(mergeCandidate)) }
func (h *mergeHeap) Pop() any {
	last := len(*h) - 1
	value := (*h)[last]
	*h = (*h)[:last]
	return value
}

func (c *Codec) mergePairs(piece string) []part {
	nodes := make([]mergeNode, len(piece)+1)
	for i := range nodes {
		nodes[i] = mergeNode{prev: i - 1, next: i + 1}
	}
	nodes[len(piece)].next = -1

	getRank := func(index int) uint {
		next := nodes[index].next
		if next < 0 || nodes[next].next < 0 {
			return math.MaxUint
		}
		if rank, ok := c.vocabulary[piece[index:nodes[next].next]]; ok {
			return rank
		}
		return math.MaxUint
	}

	candidates := make(mergeHeap, 0, len(piece))
	for i := 0; i+1 < len(piece); i++ {
		if rank := getRank(i); rank != math.MaxUint {
			candidates = append(candidates, mergeCandidate{rank: rank, index: i})
		}
	}
	heap.Init(&candidates)

	refresh := func(index int) {
		if index < 0 {
			return
		}
		nodes[index].version++
		if rank := getRank(index); rank != math.MaxUint {
			heap.Push(&candidates, mergeCandidate{rank: rank, index: index, version: nodes[index].version})
		}
	}

	remaining := len(nodes)
	for candidates.Len() > 0 {
		best := heap.Pop(&candidates).(mergeCandidate)
		if best.version != nodes[best.index].version {
			continue
		}
		next := nodes[best.index].next
		after := nodes[next].next
		nodes[best.index].next = after
		nodes[after].prev = best.index
		nodes[next].next = -1
		nodes[next].version++
		remaining--
		refresh(best.index)
		refresh(nodes[best.index].prev)
	}

	parts := make([]part, 0, remaining)
	for i := 0; i >= 0; i = nodes[i].next {
		parts = append(parts, part{offset: i, rank: math.MaxUint})
	}
	return parts
}
