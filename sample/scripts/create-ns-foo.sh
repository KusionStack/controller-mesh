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

echo "apply namespace file $tmpfile"
kubectl apply -f "$tmpfile"
rm "$tmpfile"