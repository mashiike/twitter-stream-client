package twstclient_test

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	twstclient "github.com/mashiike/twitter-stream-client"
	"github.com/stretchr/testify/assert"
)

const sampleTweet = `{"data":{"id":"1234567890","text":"hogehogehoge"},"matching_rules":[{"id": 12345680,"tag": "hoge"}]}`

func TestClient(t *testing.T) {
	log.SetFlags(log.Lshortfile)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, err := io.ReadAll(req.Body)
		assert.NoError(t, err)
		assert.EqualValues(t, "/2/tweets/search/stream", req.URL.Path)
		assert.EqualValues(t, "tweet.fields=id%2Ctext", req.URL.RawQuery)
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		time.Sleep(20 * time.Millisecond)
		io.WriteString(w, "\n")
		time.Sleep(20 * time.Millisecond)
		io.WriteString(w, sampleTweet+"\n")
		flusher.Flush()
		ctx := req.Context()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				log.Println("fuga")
				time.Sleep(20 * time.Millisecond)
				io.WriteString(w, "\n")
				flusher.Flush()
			}
		}
	}))
	defer server.Close()

	client, err := twstclient.NewClient(&twstclient.Config{
		Endpoint:    server.URL,
		BearerToken: "hogehogehoge",
	})
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	tweetCh, err := client.GetStream(ctx, url.Values{
		"tweet.fields": []string{"id,text"},
	})
	assert.NoError(t, err)
	for tweet := range tweetCh {
		if len(tweet) > 0 {
			assert.Equal(t, sampleTweet, tweet)
		}
	}
}
