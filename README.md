# buildkite-go

This is a Buildkite client that's designed to be used with Buildkite builds. It
will wait for the current Git commit to build and then tell you whether it
passed or failed.

If the build failed, we'll download the output from the failed job step and
display what happened in the terminal.

### Installation

On Mac, install with Homebrew:

```
brew install kevinburke/safe/buildkite
```

Or install from source:

```
go install github.com/kevinburke/buildkite@latest
```

If you want to get notifications when builds complete, [install the
terminal-notifier app][terminal-notifier]:

```
brew install terminal-notifier
```

[terminal-notifier]: https://github.com/julienXX/terminal-notifier

## Roadmap

Implement the features from e.g. github.com/kevinburke/go-circle, for example:

- download build artifacts
- cancel or rebuild builds on a given branch

Also add emoji support, so we can render emoji in iTerm in full fidelity.

### Configuration

You need to:

- Get a API token from https://buildkite.com/user/api-access-tokens (if your
  Buildkite repo names don't match cleanly to Github repo names; enable the
  GraphQL API for fastest responses).
- add a local config file in one of the following locations:

```
- $XDG_CONFIG_HOME/buildkite
- $HOME/cfg/buildkite
- $HOME/.buildkite
```

Put these contents in the file:

```toml
# buildkite config file: github.com/kevinburke/buildkite

# Default organization to load a token from if none of your configurations match.
default = "kevinburke"

[organizations]

    # "example" is the name of your Buildkite org, buildkite.com/example
    [organizations.example]
    token = "buildkite_token_for_example_org"

    # If your Github org name does not match the Buildkite org name, add a
    # mapping here - in this case, let's say the Github org that maps to
    # buildkite.com/example is at github.com/example_gh
    git_remotes = [
        'example_gh' # This will map github.com/example_gh => buildkite.com/example
    ]

    # If you have more than one organization, you can add other orgs/tokens
    [organizations.kevinburke]
    token = "buildkite_token_for_kevinburke"
```

### Usage

`cd` to the Git repo for your Buildkite project and then write:

```
buildkite wait
```

This will wait for your build to complete and then print out summary statistics.

`wait` exits 0 if the build passed and 1 if it failed. It exits **75**
(`EX_TEMPFAIL`) when it could not reach a verdict at all -- the API refused us
for long enough to give up, or no build was ever created for the commit. A
caller that merges on green should retry that status rather than report a
failed build.

#### Sharing an API token between concurrent waits

Buildkite's REST rate limit is enforced per API token over a fixed one-minute
window, and every consumer shares it: each concurrent `buildkite wait`, any
push hook that creates builds, and any script you run. Two things follow, and
`wait` handles both:

- A refused request (HTTP 429) is retried, waiting out the window Buildkite
  names in its response. Without this a single refusal out of thousands of
  requests surfaces as though the build had gone red.

- Polling is paced against the `RateLimit-*` headers on every response, and a
  reserve of the window is left unspent. The reserve matters more than the
  pacing: spending the last of the window does not slow *you* down, it refuses
  somebody else's request -- and if that somebody is the hook that creates
  builds, no build is created at all.

If your Buildkite pipeline slug does not match your git remote, `wait` searches
the organization for it, which costs a burst of requests. It writes the answer
to `buildkite.pipeline` in git config so the search happens once; pass
`--pipeline <slug>` to skip it entirely, or `git config --unset
buildkite.pipeline` to make it search again.

Or if you want to open the running build in your browser:

```
buildkite open
```

## Buildkite CI

This repo's Buildkite pipeline lives in [`.buildkite/pipeline.yml`](/Users/kevin/src/github.com/kevinburke/buildkite/.buildkite/pipeline.yml).
The pipeline is designed for the self-hosted agents configured by the
`buildkite_agent` role in `../caracal-server`.

In the Buildkite UI, the bootstrap pipeline should be a single step that runs:

```yaml
steps:
  - label: ":pipeline:"
    command: ".buildkite/ci/upload-pipeline.sh"
    agents:
      vm: "buildkite"
      host: "caracal"
```

The real `format`, `lint`, `test`, and `build` steps are then uploaded from the
repository.


##### Configuring browser/browser "profile"

In your config file, add the following flags to specify a browser and
a "profile":

```toml
[organizations.example]
token = "bkua_123"

// The application name of the browser you would like to open for the "open"
// command ("Google Chrome", "Firefox", "Chromium", etc).
browser_application = "Google Chrome"

// The profile name you would like to open. Chromium names these "Default",
// "Profile 1", "Profile 2", etc. To find the exact name, look in the
// "info_cache" field of e.g.
// "~/Library/Application Support/Chromium/Local State".
browser_profile = "Profile 3"
```
