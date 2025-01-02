#!/usr/bin/env bash

account=$(aws sts get-caller-identity|jq '.Account' -r)
resp=$(aws s3control list-access-points --account-id "$account"|jq '.AccessPointList|.[]|.Name' -r)

echo $resp | while read name; do
  aws s3control delete-access-point --account-id "$account" --name "$name"
done
