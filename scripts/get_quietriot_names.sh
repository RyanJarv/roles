#!/usr/bin/env bash

mkdir -p ~/.roles/names/all

curl 'https://raw.githubusercontent.com/righteousgambit/quiet-riot/refs/heads/main/wordlists/familynames-usa-top1000.txt' > ~/.roles/names/all/quiet-riot-familynames.list
curl 'https://raw.githubusercontent.com/righteousgambit/quiet-riot/refs/heads/main/wordlists/femalenames-usa-top1000.txt' > ~/.roles/names/all/quiet-riot-femalenames.list
curl 'https://raw.githubusercontent.com/righteousgambit/quiet-riot/refs/heads/main/wordlists/malenames-usa-top1000.txt' > ~/.roles/names/all/quiet-riot-malenames.list
curl 'https://raw.githubusercontent.com/righteousgambit/quiet-riot/refs/heads/main/wordlists/github-scrape.txt' > ~/.roles/names/all/quiet-riot-scrape.list
curl 'https://raw.githubusercontent.com/righteousgambit/quiet-riot/refs/heads/main/wordlists/service-linked-roles.txt' > ~/.roles/names/all/quiet-riot-service-linked-roles.list
