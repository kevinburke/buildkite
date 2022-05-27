# buildkite-go

This is a Buildkite client that's designed to be used with Buildkite builds. It
will wait for the current Git commit to build and then tell you whether it
passed or failed.

If the build failed, we'll download the output from the failed job step and
display what happened in the terminal.

### Installation

In the future we may offer compiled binaries, for now the best way is to
download the source:

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

### Configuration

You need to add a local config file. Get a API token from
https://buildkite.com/user/api-access-tokens. Once you have that, add the config
file in one of the following locations:

```
- $XDG_CONFIG_HOME/buildkite
- $HOME/cfg/buildkite
- $HOME/.buildkite
```

With these contents:

```toml
# buildkite config file: github.com/kevinburke/buildkite

# Default organization to load a token from if none of your configurations match.
default = "kevinburke"

[organizations]

    # "example" is the name of your Buildkite org, buildkite.com/example
    [organizations.example]
    token = "token_for_example_org"
    # If your Github org name does not match the Buildkite org name, add
    # a mapping here - in this case the org is at github.com/example_gh
    git_remotes = [
        'example_gh' # github.com/example_gh => buildkite.com/example
    ]

    [organizations.kevinburke]
    token = "token_for_kevinburke"
```

### Usage

`cd` to the Git repo for your Buildkite project and then write:

```
buildkite wait
```

This will wait for your build to complete and then print out summary statistics.
