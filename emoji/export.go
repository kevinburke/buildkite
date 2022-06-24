package emoji

import (
	"encoding/json"

	"github.com/kevinburke/buildkite/emoji/assets"
)

type Loader = emojiLoader

type Buildkite = buildkiteEmoji
type Apple = appleEmoji

func NewLoader() *Loader {
	// we don't need the cache path
	return &emojiLoader{cache: &emojiCache{}}
}

func (e *emojiCache) getApple() (appleEmojis, error) {
	data, err := assets.Asset("img-apple-64.json")
	if err != nil {
		return appleEmojis{}, err
	}
	ae := appleEmojis{}
	if err := json.Unmarshal(data, &ae); err != nil {
		return appleEmojis{}, err
	}
	return ae, nil
}

func (e *emojiCache) getBuildkite() (buildkiteEmojis, error) {
	data, err := assets.Asset("img-buildkite-64.json")
	if err != nil {
		return buildkiteEmojis{}, err
	}
	ae := buildkiteEmojis{}
	if err := json.Unmarshal(data, &ae); err != nil {
		return buildkiteEmojis{}, err
	}
	return ae, nil
}
