package embedding

import (
	"context"
	"fmt"
	"time"

	"github.com/tmc/langchaingo/embeddings"
	"github.com/tmc/langchaingo/llms/openai"
)

func GetEmbeddings(openAIBaseURL string, model string, apiKey string, chunk string) ([]float32, error) {

	llm, err := openai.New(openai.WithBaseURL(openAIBaseURL), openai.WithEmbeddingModel(model), openai.WithToken(apiKey))
	if err != nil {
		return nil, fmt.Errorf("error setting up LLM for embedding: %v", err)
	}

	embedder, err := embeddings.NewEmbedder(llm)

	if err != nil {
		return nil, fmt.Errorf("error creating embedder: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	vector, err := embedder.EmbedQuery(ctx, chunk)
	cancel()

	if err != nil {
		return nil, fmt.Errorf("error generating vectors: %v", err)
	}

	return vector, nil
}
