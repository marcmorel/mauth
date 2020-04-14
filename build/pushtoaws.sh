#!/bin/zsh
#to be launched from git repo root dir
docker build -t hiveway-mauth  .
docker tag hiveway-mauth:latest 744161057562.dkr.ecr.eu-west-3.amazonaws.com/hiveway-mauth:latest
$(aws ecr get-login --no-include-email --region eu-west-3)
docker push 744161057562.dkr.ecr.eu-west-3.amazonaws.com/hiveway-mauth:latest

aws ecs update-service --cluster fleetsize --service Hiveway-Mauth-Server --force-new-deployment
