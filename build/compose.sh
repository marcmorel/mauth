#/bin/bash
#to be launched from git repo root dir

#remove exiting docker-compose.yml file
rm -rf docker-compose.yml

#parse AWS credentials file to get the env variables
AWS_ACCESS_KEY_ID=$(awk -F "=" '/aws_access_key_id/ { gsub(" ","",$2); print $2}' ~/.aws/credentials)
AWS_SECRET_ACCESS_KEY=$(awk -F "=" '/aws_secret_access_key/ { gsub(" ","",$2); print $2}' ~/.aws/credentials)

#create a new docker-compose.yml from template docker-compose-template.yml
sed -e "s#{\$AWS_ACCESS_KEY_ID}#$AWS_ACCESS_KEY_ID#g" -e "s#{\$AWS_SECRET_ACCESS_KEY}#$AWS_SECRET_ACCESS_KEY#g" build/docker-compose-template.yml > docker-compose.yml

if [ $# = 1 ]
then
    #restart only containers in arguments
    docker-compose up  --no-deps --build $1
    #docker-compose up   --build $1
    rm -rf docker-compose.yml
    #docker attach $(docker ps | awk '/fleetliveapi_web/ {print $1}')
else
    #stop existing docker architecture
    docker-compose down

    #build a new one
    docker-compose build

    #start it
    docker-compose up  -d --force-recreate
    rm -rf docker-compose.yml
fi

