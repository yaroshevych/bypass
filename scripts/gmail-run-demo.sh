#!/bin/sh
# Demo secret resolution through `bypass run`; discard the child output.
set -eu

cd "$(dirname "$0")/.."
rtk proxy env GMAIL_PASSWORD='bypass://Gmail#password' \
  ./bin/bypass run \
  --identity agent-test-0466d9b0ee2ad8a57b707301a96ad342 \
  -- /usr/bin/python3 -c 'import os; assert os.environ["GMAIL_PASSWORD"]' >/dev/null
