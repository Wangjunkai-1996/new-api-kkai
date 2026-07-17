package openai

import (
	"bytes"
	"context"
	"io"

	"github.com/QuantumNous/new-api/relay/helper"
)

type openAIImageSSERead struct {
	data []byte
	done bool
	eof  bool
	err  error
}

func readOpenAIImageSSE(ctx context.Context, body io.Reader, results chan<- openAIImageSSERead, done chan<- struct{}) {
	defer close(done)
	defer close(results)

	scanner := helper.NewStreamScanner(body)
	var eventData []byte
	send := func(result openAIImageSSERead) bool {
		select {
		case results <- result:
			return true
		case <-ctx.Done():
			return false
		}
	}
	flushEvent := func() (stop bool) {
		if len(eventData) == 0 {
			return false
		}
		data := eventData
		eventData = nil
		if bytes.Equal(bytes.TrimSpace(data), []byte("[DONE]")) {
			send(openAIImageSSERead{done: true})
			return true
		}
		return !send(openAIImageSSERead{data: data})
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			if flushEvent() {
				return
			}
			continue
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		line = line[len("data:"):]
		if len(line) > 0 && line[0] == ' ' {
			line = line[1:]
		}
		if len(eventData) > 0 {
			eventData = append(eventData, '\n')
		}
		eventData = append(eventData, line...)
	}
	if flushEvent() {
		return
	}
	if err := scanner.Err(); err != nil {
		send(openAIImageSSERead{err: err})
		return
	}
	send(openAIImageSSERead{eof: true})
}
