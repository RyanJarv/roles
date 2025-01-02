#!/usr/bin/env bash

cat ~/.roles/default.json|jq '.[]|to_entries|.[]|select(.value.Exists == true)|"\(.key) # \(.value.Comment)"' -r
