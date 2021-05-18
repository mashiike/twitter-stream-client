# twitter-stream-client
client for GET  /2/tweets/search/stream

### Homebrew (macOS and Linux)

```console
$ brew install mashiike/tap/twitter-stream-client
```

### Binary packages

[Releases](https://github.com/mashiike/twitter-stream-client/releases)

## Usage

```console
Usage of pkg/twitter-stream-client--darwin-amd64:
  -bearer string
        twitter v2 API bearer token (default "*****************")
  -debug
        set log min level = debug
  -expansions expansions
        query parameters expansions
  -i    alias of --inject-created-at flag
  -inject-created-at
        inject tweet.created_at to the top level
  -media-fields media.fields
        query parameters media.fields
  -place-fields place.fields
        query parameters place.fields
  -poll-fields poll-fields
        query parameters poll-fields
  -tweet-fields tweet.fields
        query parameters tweet.fields (default "id,text")
  -user-fields user.fields
        query parameters user.fields
  -werker int
        number of worker for stream processing (default 5)
```

## Quick Start


```console
$ export TWITTER_BEARER_TOKEN=**************
$ twitter-stream-client  -i --tweet-fields=id,text,author_id,attachments,context_annotations,created_at,entities,source,public_metrics,referenced_tweets --user-fields=id,name,username,public_metrics --media-fields=url,preview_image_url,media_key --expansions=referenced_tweets.id,attachments.media_keys > tweets.json
2021/05/18 16:45:22 [INFO] start mainloop
2021/05/18 16:45:23 [INFO] connect success
2021/05/18 16:45:24 [INFO] end mainloop
... polling data
```

