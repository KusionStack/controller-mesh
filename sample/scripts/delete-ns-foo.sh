#!/bin/bash

tmpfile=$(mktemp)

for i in $(seq -w 1 32)
do
  cat << EOF >> "$tmpfile"
---
apiVersion: v1
kind: Namespace
metadata:
  name: foo-$i
EOF
done

echo "namespace file $tmpfile"
kubectl delete -f "$tmpfile"
rm "$tmpfile"