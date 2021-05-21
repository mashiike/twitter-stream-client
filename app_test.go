package twstclient_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	twstclient "github.com/mashiike/twitter-stream-client"
	"github.com/stretchr/testify/assert"
)

const errSample = `{"errors":[{"title": "operational-disconnect","disconnect_type": "UpstreamOperationalDisconnect","detail": "This stream has been disconnected upstream for operational reasons.","type": "https://api.twitter.com/2/problems/operational-disconnect"}]}`

func TestAppRun(t *testing.T) {
	log.SetFlags(log.Lshortfile)
	connectCount := 0
	tweetCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, err := io.ReadAll(req.Body)
		assert.NoError(t, err)
		assert.EqualValues(t, "/2/tweets/search/stream", req.URL.Path)
		assert.EqualValues(t, "tweet.fields=id%2Ctext", req.URL.RawQuery)
		connectCount++
		if connectCount < 4 {
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `{"Title":"hoge"}`)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		for i := 0; i < 3; i++ {
			time.Sleep(20 * time.Millisecond)
			io.WriteString(w, "\n")
			flusher.Flush()
			time.Sleep(20 * time.Millisecond)
			io.WriteString(w, sampleTweet+"\n")
			tweetCount++
			flusher.Flush()
		}
		io.WriteString(w, errSample)
	}))
	defer server.Close()
	var output bytes.Buffer
	app, err := twstclient.New(&twstclient.Config{
		Endpoint:    server.URL,
		BearerToken: "hogehogehoge",
		WorkerNum:   5,
		Output:      &output,
	})
	if !assert.NoError(t, err) {
		t.FailNow()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = app.Run(ctx)
	assert.ErrorIs(t, errors.Unwrap(err), context.DeadlineExceeded)
	tweets := strings.Split(strings.TrimRight(output.String(), "\n"), "\n")
	for _, tweet := range tweets {
		assert.JSONEq(t, sampleTweet, tweet)
	}
	assert.Equal(t, len(tweets), tweetCount)
	assert.Greater(t, connectCount, 2)
}
