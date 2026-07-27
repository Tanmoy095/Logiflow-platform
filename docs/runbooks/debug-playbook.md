   # Production Debug Playbook - LogiFlow Platform

   Maintainer: Aunmoy Dey Tanmoy

   Last Updated: 2026-07-27

   Scope: All LogiFlow microservices deployed on Kubernetes

   Target Audience: Platform Engineers, SREs, AI Coding Agents

   This playbook is the single source of truth for diagnosing and recovering from every common Kubernetes failure mode you will encounter in production. It is designed to be followed by a human under stress at 2 AM or by an AI agent autonomously executing remediation steps. Every command is explained, every symptom is mapped to its root cause, and every recovery is validated.

   ## Why Debugging Mastery Separates Seniors From Everyone Else

   A senior platform engineer does not panic at 2 AM when a service is down. They follow a deterministic sequence that surfaces the root cause in under two minutes. This sequence does not rely on intuition. It relies on understanding how Kubernetes represents failure in its API objects.

   You have already encountered several failures organically:

   - ImagePullBackOff from a non-existent image.
   - CrashLoopBackOff from a misconfigured liveness probe.
   - A corrupted liveness path (C:/Program Files/Git/healthz) caused by a Windows path leaking into Helm values.

   Each of these taught you something specific about the cluster. Now we systematise that knowledge into a reusable forensic framework.

   ## 1. The Universal Debugging Flow (Always Start Here)

   When a service is reported as unavailable, do not jump to logs. Follow this exact sequence. It is ordered by information density - the most data with the least effort.

   ```text
   Incident: "Service <name> unavailable"
               │
               ▼
   kubectl get pods -n <namespace> -l app.kubernetes.io/name=<service>
               │
               ▼ (pod exists?)
       ┌────┴────┐
      YES        NO
       │          │
       │          └─► Check Deployment: kubectl get deployment <name> -n <ns>
       │
       ▼
   kubectl describe pod <pod-name> -n <namespace>
       │  → Events section tells you the exact reason (probe failure, image pull error, OOM)
       │
       ▼
   kubectl get events -n <namespace> --sort-by='.lastTimestamp' | tail -20
       │  → Cluster-wide timeline; useful if multiple services are affected
       │
       ▼
   kubectl logs <pod-name> -n <namespace> --previous
       │  → Application output from the last crashed container
       │
       ▼
   kubectl get endpoints <service-name> -n <namespace>
       │  → Is the Service actually connected to any pod?
       │
       ▼
   kubectl describe svc <service-name> -n <namespace>
       │  → Verify selector, ports, and type
   ```

   Golden rule: The `kubectl describe pod` command works even if the container never started. The Events section at the bottom of its output gives you the root cause in plain English. Start there.

   ## 2. Failure Modes & Recovery Procedures

   Each procedure includes:

   - Symptom: what you see in `kubectl get pods`
   - Trigger: how to reproduce the failure for testing
   - Diagnosis: commands and what to look for
   - Root Cause: the underlying problem
   - Recovery: steps to restore service
   - Prevention: how the LogiFlow platform prevents this in normal operation

   ### 2.1 ImagePullBackOff

   Symptom: Pod stuck in ImagePullBackOff or ErrImagePull.

   Trigger (test): deploy with a non-existent image tag:

   ```bash
   helm upgrade --install hello-fail deployment/helm/services/hello \
      -f deployment/helm/services/hello/values.yaml \
      --set image.repository=logiflow/not-real \
      --namespace logiflow
   ```

   Diagnosis:

   ```bash
   kubectl describe pod -l app.kubernetes.io/name=hello -n logiflow
   ```

   Look at the Events section. You will see entries like:

   ```text
   Warning  Failed      kubelet  Failed to pull image "logiflow/not-real:local": rpc error: code = NotFound desc = failed to pull and unpack image ...
   Normal   BackOff     kubelet  Back-off pulling image "logiflow/not-real:local"
   Warning  Failed      kubelet  Error: ImagePullBackOff
   ```

   Why `kubectl logs` does not help: the container never started, so no logs exist.

   Root Cause: The container image referenced in the pod spec does not exist in the registry or cannot be pulled due to network/auth issues.

   Recovery:

   ```bash
   helm upgrade --install <release> deployment/helm/services/<service> \
      -f deployment/helm/services/<service>/values.yaml \
      --namespace <namespace>
   ```

   This reinstates the correct image tag from the values file. Alternatively, if you want to keep the release but correct only the image tag, use `helm upgrade --set image.tag=<correct-tag>`.

   Prevention (Platform Level): The library chart enforces the image field, but does not validate its existence. In CI, `docker build && kind load docker-image` ensures the image exists locally. For production, image tags should be immutable and validated by the CI pipeline before deployment.

   ### 2.2 CrashLoopBackOff (Startup Probe Failure)

   Symptom: Pod restarts multiple times; STATUS shows CrashLoopBackOff.

   Trigger (test): point the startup probe to a non-existent endpoint:

   ```bash
   helm upgrade --install hello-fail deployment/helm/services/hello \
      -f deployment/helm/services/hello/values.yaml \
      --set probes.startup.path=/nonexistent \
      --namespace logiflow
   ```

   Diagnosis:

   ```bash
   kubectl describe pod -l app.kubernetes.io/name=hello -n logiflow
   ```

   Events:

   ```text
   Warning  Unhealthy  kubelet  Startup probe failed: HTTP probe failed with statuscode: 404
   Normal   Killing    kubelet  Container hello failed startup probe, will be restarted
   ```

   Also check the Restart Count in the pod status. Note that the readiness and liveness probes never execute because the startup probe has not succeeded.

   Root Cause: The application cannot finish its startup sequence within the allowed `failureThreshold * periodSeconds` window. This is either because a genuine crash occurs during boot, or more commonly the probe endpoint is misconfigured.

   Recovery:

   ```bash
   helm upgrade --install <release> deployment/helm/services/<service> \
      -f deployment/helm/services/<service>/values.yaml \
      --namespace <namespace>
   ```

   If the service genuinely needs longer to start, for example AI model loading, increase `probes.startup.failureThreshold` in the service's values file.

   Prevention: The library chart provides a default startup probe with `failureThreshold: 30` and `periodSeconds: 5` (150 s patience). Services must only provide a valid path. For AI workloads, the template allows overriding `failureThreshold`.

   ### 2.3 CrashLoopBackOff (Liveness Probe Failure)

   Symptom: Pod becomes Running and Ready initially, then is killed and restarted repeatedly. STATUS shows CrashLoopBackOff.

   Trigger (test): point the liveness probe to a non-existent endpoint while keeping startup/readiness correct:

   ```bash
   helm upgrade --install hello-fail deployment/helm/services/hello \
      -f deployment/helm/services/hello/values.yaml \
      --set probes.liveness.path=/nonexistent \
      --namespace logiflow
   ```

   Diagnosis:

   ```bash
   kubectl describe pod -l app.kubernetes.io/name=hello -n logiflow
   ```

   Events:

   ```text
   Warning  Unhealthy  kubelet  Liveness probe failed: HTTP probe failed with statuscode: 404
   Normal   Killing    kubelet  Container hello failed liveness probe, will be restarted
   ```

   Note: this pattern is different from a startup probe failure because the pod did become Ready. The liveness probe fails during normal operation.

   Root Cause: The application is alive, meaning the process is running, but the liveness probe endpoint returns an error. This can be a deadlocked server, a misconfigured endpoint, or a genuine application bug that causes the health check to fail.

   Recovery:

   ```bash
   helm upgrade --install <release> deployment/helm/services/<service> \
      -f deployment/helm/services/<service>/values.yaml \
      --namespace <namespace>
   ```

   If the problem is a genuine deadlock, the liveness probe restarting the container will eventually fix it by forcing a fresh start. But if the probe is misconfigured, the cycle will repeat indefinitely until corrected.

   Prevention: The library chart enforces that liveness and readiness paths are separate. The default liveness path `/live` must be implemented by the application. If the app does not have a dedicated liveness endpoint, the service should override it to an existing endpoint like `/healthz` in its values file.

   ### 2.4 Readiness Probe Failure (Pod Running but Not Ready)

   Symptom: Pod is Running, but READY column shows 0/1. Service endpoints are empty; traffic is not reaching the pod.

   Trigger (test): point the readiness probe to a non-existent endpoint:

   ```bash
   helm upgrade --install hello-fail deployment/helm/services/hello \
      -f deployment/helm/services/hello/values.yaml \
      --set probes.readiness.path=/nonexistent \
      --namespace logiflow
   ```

   Diagnosis:

   ```bash
   kubectl get pods -n logiflow                         # READY 0/1
   kubectl get endpoints <release> -n logiflow           # <none>
   kubectl describe pod -l app.kubernetes.io/name=hello -n logiflow
   ```

   Events:

   ```text
   Warning  Unhealthy  kubelet  Readiness probe failed: HTTP probe failed with statuscode: 404
   ```

   The container is not killed. This is critical: readiness controls traffic, liveness controls restarts.

   Root Cause: The application is alive, but a dependency such as the database, message broker, or cache is unavailable, or the readiness endpoint is misconfigured.

   Recovery:

   ```bash
   helm upgrade --install <release> deployment/helm/services/<service> \
      -f deployment/helm/services/<service>/values.yaml \
      --namespace <namespace>
   ```

   Prevention: Services must implement a proper readiness endpoint that returns 200 only when all required dependencies are reachable. The library chart's default `/healthz` is a good starting point, but complex services should override it.

   ### 2.5 OOMKilled (Memory Limit Exceeded)

   Symptom: Pod is terminated with reason OOMKilled. The pod may be restarted if it is part of a Deployment.

   Trigger (test): run a pod that exceeds its memory limit:

   ```bash
   kubectl run oom-test --image=busybox --restart=Never --limits='memory=32Mi' \
      -- sh -c "dd if=/dev/zero of=/dev/null bs=50M count=10"
   ```

   Diagnosis:

   ```bash
   kubectl describe pod oom-test
   ```

   Look for:

   ```text
   State:          Terminated
      Reason:       OOMKilled
      Exit Code:    137
   ```

   Exit code 137 = 128 + 9 (SIGKILL). The kernel's Out-Of-Memory killer terminated the process because the container's memory usage exceeded the cgroup limit.

   Root Cause: The container's actual memory consumption exceeds the `resources.limits.memory` value. This could be a memory leak or a misconfigured limit.

   Recovery:

   - Increase the memory limit in the service's values file (`resources.limits.memory`).
   - If the OOM is due to a leak, fix the application code.

   Prevention: The library chart sets default memory limits (128Mi request, 256Mi limit). Services that process large payloads, for example AI models or document processing, should adjust these in their environment-specific values files. Monitoring tools such as Prometheus should alert on memory usage approaching the limit.

   ### 2.6 CPU Starvation (Probe Timeouts / Slow Startup)

   Symptom: Pod takes an unusually long time to become Ready, or probes fail with `context deadline exceeded`.

   Trigger (test): set a very low CPU limit:

   ```bash
   helm upgrade --install hello-fail deployment/helm/services/hello \
      -f deployment/helm/services/hello/values.yaml \
      --set resources.requests.cpu=10m \
      --set resources.limits.cpu=10m \
      --namespace logiflow
   ```

   Diagnosis:

   ```bash
   kubectl describe pod -l app.kubernetes.io/name=hello -n logiflow
   ```

   Look for probe failure events containing `context deadline exceeded` or unusually high response times. If a metrics server is available, `kubectl top pod` will show CPU usage near the limit.

   Root Cause: The CPU limit is too low for the application to perform its normal work, including responding to health check requests in time.

   Recovery:

   ```bash
   helm upgrade --install <release> deployment/helm/services/<service> \
      -f deployment/helm/services/<service>/values.yaml \
      --namespace <namespace>
   ```

   Increase `resources.limits.cpu` and possibly `requests.cpu` to a value that allows the application to operate normally.

   Prevention: Load-test new services to determine realistic resource requirements. The library chart's defaults (100m/200m CPU) work for typical Go microservices, but AI and data-processing services must be tuned.

   ### 2.7 Broken Service Selector (Empty Endpoints)

   Symptom: Pods are Running and Ready, but the Service has no endpoints (`<none>`). Traffic is black-holed.

   Trigger (test): patch the Service selector to a non-matching label:

   ```bash
   kubectl patch svc <release> -n logiflow -p '{"spec":{"selector":{"app.kubernetes.io/name":"does-not-exist"}}}'
   ```

   Diagnosis:

   ```bash
   kubectl get endpoints <release> -n logiflow        # <none>
   kubectl describe svc <release> -n logiflow         # Check Selector field
   kubectl get pods -n logiflow --show-labels          # See actual pod labels
   ```

   Root Cause: The Service's selector field does not match the labels on any ready pods. The Endpoints controller cannot link them.

   Recovery:

   ```bash
   # Patch back to the correct selector (use the library's selectorLabels)
   kubectl patch svc <release> -n logiflow -p '{"spec":{"selector":{"app.kubernetes.io/name":"hello"}}}'

   # Or redeploy the chart (which always has the correct selector)
   helm upgrade --install <release> deployment/helm/services/<service> \
      -f deployment/helm/services/<service>/values.yaml \
      --namespace logiflow
   ```

   Prevention: The library chart provides a single `logiflow.selectorLabels` helper used in both the Deployment and the Service. Services should never manually edit selectors. The golden rule: the same helper must appear in both templates. Our generation script and library chart enforce this automatically.

   ## 3. Essential Debugging Command Reference

   | Command | Purpose | When to Use |
   | --- | --- | --- |
   | `kubectl get pods -n <ns> -l app.kubernetes.io/name=<svc>` | Quick health overview of a service's pods | First command after an alert |
   | `kubectl describe pod <pod> -n <ns>` | Detailed pod status, events, probe config | For any pod not in Running/Ready |
   | `kubectl get events -n <ns> --sort-by='.lastTimestamp' | tail -20` | Recent cluster-level events | When multiple pods/services are affected |
   | `kubectl logs <pod> -n <ns> --previous` | Application logs from the last crashed container | After describe shows a terminated container |
   | `kubectl get endpoints <svc> -n <ns>` | Backend pod IPs for a Service | When traffic is not reaching the pods |
   | `kubectl describe svc <svc> -n <ns>` | Service configuration (selector, ports, type) | When endpoints are empty or traffic misrouted |
   | `kubectl top pod -n <ns>` | CPU/Memory usage (requires metrics-server) | Suspected resource starvation |
   | `kubectl get deployment <name> -n <ns> -o yaml` | Live desired state vs. Helm template | When you suspect configuration drift |
   | `helm history <release> -n <ns>` | Recent Helm revisions | To identify what changed and when |
   | `helm rollback <release> <revision> -n <ns>` | Instant revert to a previous state | If a recent upgrade caused the problem |
   | `helm template <release> <chart> -f <values> -n <ns>` | Render chart locally without deploying | Pre-deployment validation; compare with live state |

   ## 4. AI Agent Debugging Workflow (AgentOps)

   This section describes the exact sequence an AI coding agent, or an autonomous operations agent, should follow when it receives an alert like “Service shipment unavailable”. It mirrors the human workflow but is structured to be parsed and executed programmatically.

   ### 4.1 Agent Decision Tree

   ```text
   ALERT: "Service shipment unavailable"
               │
               ▼
   1. kubectl get pods -n logiflow -l app.kubernetes.io/name=shipment
               │
       ┌────┴────┐
      Pods exist?  NO → check Deployment: kubectl get deployment shipment -n logiflow
       │                → if missing, suggest: helm install shipment ...
       │
       ▼ (pods exist)
   2. kubectl describe pod <pod-name> -n logiflow
               │
               ▼
       Parse Events:
       ├── "Failed to pull image" → ImagePullBackOff
       │     action: alert "ImagePullBackOff - verify image tag in values.yaml"
       │
       ├── "Startup probe failed" → CrashLoopBackOff (startup)
       │     action: alert "Startup probe failing - check probes.startup.path"
       │
       ├── "Liveness probe failed" → CrashLoopBackOff (liveness)
       │     action: alert "Liveness probe failing - check probes.liveness.path"
       │
       ├── "Readiness probe failed" → Pod Running but Not Ready
       │     action: alert "Readiness probe failing - check probes.readiness.path and dependencies"
       │
       ├── "OOMKilled" → Memory limit exceeded
       │     action: alert "OOMKilled - consider increasing memory limit"
       │
       └── "context deadline exceeded" / timeouts → CPU starvation
                action: alert "Probe timeouts - CPU limit may be too low"

   3. kubectl get endpoints shipment -n logiflow
               │
               ▼
       Endpoints empty? YES → kubectl describe svc shipment -n logiflow
               │                    → compare selector with pod labels
               │                    → if mismatch, suggest fix: restore correct selector
               │
               ▼
   4. If no infrastructure issue found:
       kubectl logs <pod> -n logiflow --previous
       → look for panic, error logs, stack traces
       → if application error, suggest rollback: helm rollback shipment <rev>

   5. If fix requires changing resources (limits, replicas) or code:
       → STOP and request human approval before proceeding.
       → If fix is a safe configuration change (probe path, image tag), proceed with helm upgrade.
   ```

   ### 4.2 Why This Flow Works for Both Humans and Agents

   - Deterministic order: every step depends on the output of the previous one.
   - Plain-English event parsing: Kubernetes Events are stable, well-known strings.
   - Minimal privilege escalation: the agent only makes changes that are proven safe; destructive changes require human review.
   - Same platform contracts: because all services use the same library chart (labels, probes, selectors), the agent can rely on consistent naming and configuration patterns.

   ## 5. Prevention: How the LogiFlow Platform Reduces These Failures

   | Failure Mode | Platform Prevention |
   | --- | --- |
   | ImagePullBackOff | CI builds and loads images into Kind; immutable tags enforced in production. |
   | CrashLoopBackOff (probes) | Library chart provides safe defaults and a combined probe template; developers only set paths. |
   | Readiness failure | Service skeleton encourages a proper readiness endpoint; probes use separate endpoints. |
   | OOMKilled | Library chart sets reasonable memory defaults; services can override per environment. |
   | CPU Starvation | Default CPU requests/limits are applied; load testing is part of the deploy-first checklist. |
   | Broken selector | Library's selectorLabels helper is used in both Deployment and Service; generation script ensures consistency. |

   ## 6. Final Clean Slate Command (Run After Labs)

   To reset the cluster to a healthy baseline:

   ```bash
   helm uninstall hello-fail -n logiflow 2>/dev/null
   helm upgrade --install hello deployment/helm/services/hello \
      -f deployment/helm/services/hello/values.yaml \
      --set probes.liveness.path=/healthz \
      --namespace logiflow
   kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=hello -n logiflow --timeout=60s
   ```