## v0.28 (September 10, 2026)

Survive a shared rate limit window, so that several `buildkite wait` runs can
go at once.

- Retry a request Buildkite refused with a 429, waiting out the window it
  names in `Retry-After` or `RateLimit-Reset`. A 429 refuses the request
  rather than performing it, so replaying it is safe even for a write; a 5xx
  is only retried for reads, since the server may have done the work before
  failing to say so.

- Pace the wait's polling against the rate limit headers on every response.
  The window belongs to the API token, not to a process, so those headers are
  the only coordination signal several concurrent waits have. A reserve is
  held back for everything else drawing on the token -- above all the git
  post-receive hook, whose build POST being refused means no build is created
  at all.

- Wait for a build to be created instead of failing the instant one is
  missing. A commit pushed a moment ago has no build yet, and reporting that
  as a failure made every rebase a coin flip.

- Exit 75 (EX_TEMPFAIL) when no verdict could be reached -- throttled, or no
  build was ever created -- so callers can tell that apart from a red build.
  Both used to exit 1, which read as a broken change.

- Only search for a pipeline when the configured one does not exist or has
  never built anything; a missing build for one commit is no longer reason to
  scan the organization. Cache the result in `buildkite.pipeline` in git
  config, and accept `--pipeline` to skip the search outright.

## v0.25 (January 27, 2026)

Improve resilience in the event of network failure (try longer and catch more
types of errors).
