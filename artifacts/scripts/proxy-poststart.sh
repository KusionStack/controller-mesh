#!/usr/bin/env bash

OUTPUT=/proc/1/fd/1
[ -w /proc/1/fd/1 ] || OUTPUT=/dev/null
MSG=""
counter=0
max=50
until curl --output /dev/null --silent --head --fail http://127.0.0.1:5441/readyz; do
    if [ ${counter} -eq ${max} ];then
      MSG="${MSG} run poststart.sh failed, max attempts reached!"
      echo "${MSG}" | tee -a "${OUTPUT}"
      exit 0
    fi
    MSG="${MSG}."
    counter=$((counter+1))
    sleep 2
done
