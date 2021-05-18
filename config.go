package twstclient

import (
	"errors"
	"io"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	Endpoint        string
	BearerToken     string
	WorkerNum       int
	QueryParams     url.Values
	Output          io.Writer
	InjectCreatedAt bool
}

func (config *Config) Validate() error {
	if config.BearerToken == "" {
		return errors.New("BearerToken is required")
	}
	if config.Endpoint == "" {
		config.Endpoint = "https://api.twitter.com"
	}
	if config.WorkerNum <= 0 {
		config.WorkerNum = 5
	}
	if config.Output == nil {
		config.Output = os.Stdout
	}
	if config.QueryParams == nil {
		config.QueryParams = url.Values{
			"tweet.fields": []string{"id,text"},
		}
	}
	if config.InjectCreatedAt {
		tweetFileds := config.QueryParams.Get("tweet.fields")
		if tweetFileds == "" {
			tweetFileds = "created_at"
		}
		fileds := strings.Split(tweetFileds, ",")
		exists := false
		for _, field := range fileds {
			if field == "created_at" {
				exists = true
			}
		}
		if !exists {
			fileds = append(fileds, "created_at")
		}
		config.QueryParams.Set("tweet.fields", strings.Join(fileds, ","))
	}

	return nil
}
