package embedding

import (
	"testing"
)

func TestChunkText(t *testing.T) {
	text := "Lorem ipsum dolor sit, amet consectetur adipisicing elit. Nam velit doloremque itaque, aliquid distinctio dolore ex quaerat quia, totam cupiditate impedit placeat hic iusto fugiat consequuntur non nobis eaque aspernatur?"

	for chunkSize := 5; chunkSize < 50; chunkSize += 5 {
		for overlap := 0; overlap <= chunkSize/2; overlap++ {
			results, err := ChunkText(text, chunkSize, overlap)

			if err != nil {
				t.Fatalf("error chunking text: %v\n", err)
			}

			for _, str := range results {
				if CountTokens(str) > chunkSize {
					t.Fatalf("chunk is larger than chunkSize\n")
				}
			}
		}
	}
}
