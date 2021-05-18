package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/url"
	"os"
	"os/signal"

	"github.com/hashicorp/logutils"
	"github.com/mashiike/didumean"
	twstclient "github.com/mashiike/twitter-stream-client"
)

const (
	DebugLevel = "DEBUG"
	InfoLevel  = "INFO"
	ErrorLevel = "ERROR"
)

func main() {
	var (
		bearer                                                                    string
		expansions, mediaFields, placeFields, pollFields, tweetFields, userFields string
		workerNum                                                                 int
		inject, debug                                                             bool
	)
	defaultToken := os.Getenv("TWITTER_BEARER_TOKEN")
	flag.StringVar(&bearer, "bearer", defaultToken, "twitter v2 API bearer token")
	flag.IntVar(&workerNum, "werker", 5, "number of worker for stream processing")
	flag.BoolVar(&inject, "inject-created-at", false, "inject tweet.created_at to the top level")
	flag.BoolVar(&inject, "i", false, "alias of --inject-created-at flag")
	flag.BoolVar(&debug, "debug", false, "set log min level = debug")

	flag.StringVar(&expansions, "expansions", "", "query parameters `expansions`")
	flag.StringVar(&mediaFields, "media-fields", "", "query parameters `media.fields`")
	flag.StringVar(&placeFields, "place-fields", "", "query parameters `place.fields`")
	flag.StringVar(&pollFields, "poll-fields", "", "query parameters `poll-fields`")
	flag.StringVar(&tweetFields, "tweet-fields", "id,text", "query parameters `tweet.fields`")
	flag.StringVar(&userFields, "user-fields", "", "query parameters `user.fields`")
	didumean.Parse()

	minLevel := InfoLevel
	if debug {
		minLevel = DebugLevel
		log.SetFlags(log.Lshortfile)
	}
	filter := &logutils.LevelFilter{
		Levels:   []logutils.LogLevel{DebugLevel, InfoLevel, ErrorLevel},
		MinLevel: logutils.LogLevel(minLevel),
		Writer:   os.Stderr,
	}
	log.SetOutput(filter)
	params := url.Values{}
	if expansions != "" {
		params.Set("expansions", expansions)
	}
	if mediaFields != "" {
		params.Set("media.fields", mediaFields)
	}
	if placeFields != "" {
		params.Set("place.fields", placeFields)
	}
	if pollFields != "" {
		params.Set("poll.fields", pollFields)
	}
	if tweetFields != "" {
		params.Set("tweet.fields", tweetFields)
	}
	if userFields != "" {
		params.Set("user.fields", userFields)
	}

	config := &twstclient.Config{
		BearerToken:     bearer,
		WorkerNum:       workerNum,
		InjectCreatedAt: inject,
		QueryParams:     params,
	}

	app, err := twstclient.New(config)
	if err != nil {
		log.Fatalf("[ERROR] %s", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := app.Run(ctx); err != nil {
		unwrapedErr := errors.Unwrap(err)
		if unwrapedErr == context.Canceled || unwrapedErr == context.DeadlineExceeded {
			return
		}
		log.Fatalf("[ERROR] %s", err)
	}

}
