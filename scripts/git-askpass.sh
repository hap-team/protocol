#!/bin/sh
# Git invokes this helper inside the isolated build. The token never enters a URL.
case "$1" in
  *Username*) printf '%s\n' x-access-token ;;
  *Password*) cat /run/secrets/github-token ;;
  *) exit 1 ;;
esac
