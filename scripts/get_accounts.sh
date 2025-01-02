#!/usr/bin/env bash
URL='https://raw.githubusercontent.com/rupertbg/aws-public-account-ids/master/accounts.json'

curl "$URL" | jq '.[]|select(.owner != "Amazon Web Services")|"\(.id) # \(.owner) \(.source)"' -r


# https://github.com/duo-labs/cloudmapper/blob/main/vendor_accounts.yaml
# convert to json
# jq -r '.[]| select(.type != "aws") | .accounts[] as $account | {name: .name, source: .source} | "\($account): \(.name), \(.source)"'
