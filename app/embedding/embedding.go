package embedding

import (
	"context"
	"fmt"

	"github.com/tmc/langchaingo/embeddings"
	"github.com/tmc/langchaingo/llms/openai"
)

func GetEmbeddings(ctx context.Context, openAIBaseURL string, model string, apiKey string, chunks []string) ([][]float32, error) {
	if apiKey == "" {
		// `langchaingo` emits an error when the OpenAI API key is empty, even if the API URL has been changed to one that doesn't require authentication.
		apiKey = "-"
	}
	llm, err := openai.New(openai.WithBaseURL(openAIBaseURL), openai.WithEmbeddingModel(model), openai.WithToken(apiKey))
	if err != nil {
		return nil, fmt.Errorf("error setting up LLM for embedding: %v", err)
	}

	embedder, err := embeddings.NewEmbedder(llm)

	if err != nil {
		return nil, fmt.Errorf("error creating embedder: %v", err)
	}

	vectors, err := embedder.EmbedDocuments(ctx, chunks)

	if err != nil {
		return nil, fmt.Errorf("error generating vectors: %v", err)
	}

	return vectors, nil
}
