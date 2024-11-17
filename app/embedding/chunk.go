package embedding

import (
	"fmt"

	"github.com/pkoukk/tiktoken-go"
	tiktoken_loader "github.com/pkoukk/tiktoken-go-loader"
	"github.com/tmc/langchaingo/textsplitter"
)

var encoder *tiktoken.Tiktoken

func init() {
	tiktoken.SetBpeLoader(tiktoken_loader.NewOfflineLoader())
	var err error
	encoder, err = tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		panic(fmt.Sprintf("error loading encoding: %v", err))
	}
}

// https://python.langchain.com/docs/how_to/recursive_text_splitter/#splitting-text-from-languages-without-word-boundaries
var separators = []string{
	"\n\n",
	"\n",
	" ",
	".",
	",",
	"\u200b", // Zero-width space
	"\uff0c", // Fullwidth comma
	"\u3001", // Ideographic comma
	"\uff0e", // Fullwidth full stop
	"\u3002", // Ideographic full stop
	"",
}

func ChunkText(text string, chunkSize int, overlap int) ([]string, error) {
	splitter := textsplitter.NewRecursiveCharacter()
	splitter.ChunkSize = chunkSize
	splitter.ChunkOverlap = overlap
	splitter.KeepSeparator = false
	splitter.Separators = separators
	splitter.LenFunc = CountTokens

	return splitter.SplitText(text)
}

func CountTokens(s string) int {
	return len(encoder.Encode(s, nil, nil))
}
