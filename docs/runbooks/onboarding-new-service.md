# 1. Copy the hello chart
cp -r deployment/helm/services/hello deployment/helm/services/<service-name>

# 2. Clean old dependencies
cd deployment/helm/services/<service-name>
rm -rf charts/ Chart.lock
cd -

# 3. Edit Chart.yaml: change name and description

# 4. Edit values.yaml: set image, port, and custom config

# 5. Edit templates to reference service‑specific env vars (if any)

# 6. Fetch library
cd deployment/helm/services/<service-name>
helm dependency update
cd -

# 7. Validate
helm lint deployment/helm/services/<service-name> --namespace logiflow
helm template <release-name> deployment/helm/services/<service-name> \
  --namespace logiflow --set image.repository=... --set image.tag=... --set service.port=...

# 8. Deploy (ensure image exists)
helm upgrade --install <release-name> deployment/helm/services/<service-name> \
  --namespace logiflow \
  --set image.repository=... --set image.tag=... --set service.port=... \
  --wait --timeout 120s