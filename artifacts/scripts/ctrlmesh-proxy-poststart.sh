#!/usr/bin/env bash

OUTPUT=/proc/1/fd/1
[ -w /proc/1/fd/1 ] || OUTPUT=/dev/null
METRICS_HEALTH_PORT=${METRICS_HEALTH_PORT:-5441}
MSG=""

# Wait for ApiServer proxy ready
attempt_counter=0
max_attempts=60
until curl --output /dev/null --silent --head --fail http://127.0.0.1:"${METRICS_HEALTH_PORT}"/readyz; do
    if [ ${attempt_counter} -eq ${max_attempts} ];then
      MSG="${MSG} POST START failed, max attempts reached!"
      echo "${MSG}" | tee -a "${OUTPUT}"
      exit 0
    fi

    MSG="${MSG}."
    attempt_counter=$((attempt_counter+1))
    sleep 3
done
