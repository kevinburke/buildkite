Steps to import:

1. run "make emoji"

2. Change "package local" to "package emoji"

3. Replace "buildkiteEmojis()" and "appleEmojis()" with "e.cache.getBuildkite()"
   and "e.cache.getApple()"
