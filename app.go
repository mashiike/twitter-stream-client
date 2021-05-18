package twstclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/lestrrat-go/backoff/v2"
)

type App struct {
	client          *Client
	workerNum       int
	params          url.Values
	encoder         *json.Encoder
	injectCreatedAt bool
}

func New(config *Config) (*App, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	c, err := NewClient(config)
	if err != nil {
		return nil, err
	}
	encoder := json.NewEncoder(config.Output)
	app := &App{
		client:          c,
		workerNum:       config.WorkerNum,
		params:          config.QueryParams,
		encoder:         encoder,
		injectCreatedAt: config.InjectCreatedAt,
	}
	return app, nil
}

func (app *App) Run(ctx context.Context) error {
	tweetCh := make(chan string, app.workerNum*2)
	errCh := make(chan error, app.workerNum*2)
	var wg sync.WaitGroup
	for i := 0; i < app.workerNum; i++ {
		workerID := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Printf("[DEBUG] werker_id=%d start\n", workerID)
			app.worker(tweetCh, errCh)
			log.Printf("[DEBUG] werker_id=%d end\n", workerID)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Println("[DEBUG] start worker error reporter")
		app.errorReporter(errCh)
		log.Println("[DEBUG] end worker error reporter")
	}()
	done := false
	var err error
	for !done {
		log.Println("[INFO] start mainloop")
		err = app.mainLoop(ctx, tweetCh)
		log.Println("[INFO] end mainloop")
		if err != nil {
			log.Printf("[ERROR] %s", err.Error())
			done = true
			continue
		}
		select {
		case <-ctx.Done():
			done = true
			continue
		default:
			log.Println("[INFO] 5 sec after try restart mainloop")
			time.Sleep(5 * time.Second)
		}
	}
	close(tweetCh)
	close(errCh)
	wg.Wait()
	return err
}

func (app *App) mainLoop(ctx context.Context, tweetCh chan<- string) error {
	respCh, err := app.getStream(ctx)
	if err != nil {
		return err
	}
	log.Println("[INFO] connect success")
	for {
		resp, ok := <-respCh
		if !ok {
			return nil
		}
		if len(resp) <= 0 {
			log.Println("[DEBUG] heartbeet")
			continue
		}
		tweetCh <- resp
	}
}

func (app *App) errorReporter(errCh <-chan error) {
	for {
		err, ok := <-errCh
		if ok {
			log.Printf("[INFO] %s", err.Error())
		} else {
			return
		}
	}
}

type respData struct {
	Data          map[string]interface{} `json:"data,omitempty"`
	MatchingRules interface{}            `json:"matching_rules,omitempty"`
	CreatedAt     interface{}            `json:"created_at,omitempty"`
	Errors        []json.RawMessage      `json:"errors,omitempty"`
}

func (app *App) worker(tweetCh <-chan string, errCh chan<- error) {
	for {
		tweet, ok := <-tweetCh
		if !ok {
			return
		}
		var data respData
		decoder := json.NewDecoder(strings.NewReader(tweet))
		if err := decoder.Decode(&data); err != nil {
			errCh <- err
			continue
		}
		if len(data.Errors) > 0 {
			for _, err := range data.Errors {
				errCh <- fmt.Errorf("%s", string(err))
			}
			continue
		}
		if app.injectCreatedAt {
			if createdAt, ok := data.Data["created_at"]; ok {
				data.CreatedAt = createdAt
			}
		}
		if err := app.encoder.Encode(data); err != nil {
			errCh <- err
			continue
		}
	}
}

func (app *App) getStream(ctx context.Context) (<-chan string, error) {
	respCh, err := app.client.GetStream(ctx, app.params)
	if err != nil {
		log.Printf("[ERROR] GetStream failed: %s\n", err.Error())
		//https://developer.twitter.com/en/docs/twitter-api/tweets/filtered-stream/integrate/handling-disconnections
		minInterval := 250 * time.Millisecond
		maxInterval := 16 * time.Second
		switch err := err.(type) {
		case *responseError:
			switch err.StatusCode {
			case http.StatusTooManyRequests:
				minInterval = time.Minute
				maxInterval = 16 * time.Minute
			case http.StatusInternalServerError:
			default:
				return nil, err
			}
		default:
			internalErr := errors.Unwrap(err)
			if internalErr == context.Canceled || internalErr == context.DeadlineExceeded {
				return nil, err
			}
		}
		p := backoff.Exponential(
			backoff.WithMinInterval(minInterval),
			backoff.WithMaxInterval(maxInterval),
			backoff.WithJitterFactor(0.05),
			backoff.WithMultiplier(2.0),
		)
		controller := p.Start(ctx)
		var i int = 0
		for backoff.Continue(controller) {
			i++
			respCh, err = app.client.GetStream(ctx, app.params)
			if err == nil || errors.Unwrap(err) == context.Canceled || errors.Unwrap(err) == context.DeadlineExceeded {
				return respCh, nil
			}
			log.Printf("[ERROR] RetryCount=%d, GetStream failed: %s\n", i, err.Error())
		}
		return nil, err
	}
	return respCh, nil
}