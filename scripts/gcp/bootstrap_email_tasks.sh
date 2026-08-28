#!/usr/bin/env bash
set -euo pipefail

PROJECT="${PROJECT:-Selecto-development}"
REGION="${REGION:-southamerica-east1}"
API_SERVICE="${API_SERVICE:-selecto-ecommerce-staging}"
WORKER_SERVICE="${WORKER_SERVICE:-Selecto-email-outbox-worker-staging}"
FALLBACK_JOB="${FALLBACK_JOB:-Selecto-email-outbox-staging}"
FALLBACK_SCHEDULER="${FALLBACK_SCHEDULER:-Selecto-email-outbox-staging}"
QUEUE="${QUEUE:-Selecto-email-outbox-staging}"
TASK_SERVICE_ACCOUNT="${TASK_SERVICE_ACCOUNT:-Selecto-email-tasks@${PROJECT}.iam.gserviceaccount.com}"
WORKER_SERVICE_ACCOUNT="${WORKER_SERVICE_ACCOUNT:-Selecto-email-worker@${PROJECT}.iam.gserviceaccount.com}"
DATABASE_SECRET="${DATABASE_SECRET:-selecto-ecommerce-database-url}"
SMTP_PASSWORD_SECRET="${SMTP_PASSWORD_SECRET:-SMTP_PASSWORD}"
SMTP_HOST="${SMTP_HOST:-smtp-relay.brevo.com}"
SMTP_PORT="${SMTP_PORT:-587}"
SMTP_USERNAME="${SMTP_USERNAME:?Set SMTP_USERNAME to the Brevo SMTP login}"
SMTP_FROM="${SMTP_FROM:-Selecto <no-reply@Selecto.store>}"

gcloud config set project "${PROJECT}" >/dev/null
gcloud services enable cloudtasks.googleapis.com run.googleapis.com iam.googleapis.com

IMAGE="${IMAGE:-$(gcloud run services describe "${API_SERVICE}" \
  --region="${REGION}" --format='value(spec.template.spec.containers[0].image)')}"
API_RUNTIME_SERVICE_ACCOUNT="${API_RUNTIME_SERVICE_ACCOUNT:-$(gcloud run services describe "${API_SERVICE}" \
  --region="${REGION}" --format='value(spec.template.spec.serviceAccountName)')}"
API_SERVICE_JSON="$(gcloud run services describe "${API_SERVICE}" \
  --region="${REGION}" --format=json)"

NETWORK_INTERFACES="$(printf '%s' "${API_SERVICE_JSON}" | jq -r \
  '.spec.template.metadata.annotations["run.googleapis.com/network-interfaces"] // empty')"
VPC_CONNECTOR="$(printf '%s' "${API_SERVICE_JSON}" | jq -r \
  '.spec.template.metadata.annotations["run.googleapis.com/vpc-access-connector"] // empty')"
VPC_EGRESS="$(printf '%s' "${API_SERVICE_JSON}" | jq -r \
  '.spec.template.metadata.annotations["run.googleapis.com/vpc-access-egress"] // "private-ranges-only"')"

NETWORK_ARGS=()
if [[ -n "${NETWORK_INTERFACES}" ]]; then
  NETWORK="$(printf '%s' "${NETWORK_INTERFACES}" | jq -r '.[0].network')"
  SUBNET="$(printf '%s' "${NETWORK_INTERFACES}" | jq -r '.[0].subnetwork')"
  NETWORK_ARGS=(--network="${NETWORK}" --subnet="${SUBNET}" --vpc-egress="${VPC_EGRESS}")
elif [[ -n "${VPC_CONNECTOR}" ]]; then
  NETWORK_ARGS=(--vpc-connector="${VPC_CONNECTOR}" --vpc-egress="${VPC_EGRESS}")
else
  printf 'The API service has no private VPC configuration to inherit.\n' >&2
  exit 1
fi

if ! gcloud iam service-accounts describe "${TASK_SERVICE_ACCOUNT}" >/dev/null 2>&1; then
  gcloud iam service-accounts create "${TASK_SERVICE_ACCOUNT%%@*}" \
    --display-name="Selecto email task invoker"
fi
if ! gcloud iam service-accounts describe "${WORKER_SERVICE_ACCOUNT}" >/dev/null 2>&1; then
  gcloud iam service-accounts create "${WORKER_SERVICE_ACCOUNT%%@*}" \
    --display-name="Selecto email worker runtime"
fi

if ! gcloud tasks queues describe "${QUEUE}" --location="${REGION}" >/dev/null 2>&1; then
  gcloud tasks queues create "${QUEUE}" --location="${REGION}"
fi
gcloud tasks queues update "${QUEUE}" --location="${REGION}" \
  --max-dispatches-per-second=10 --max-concurrent-dispatches=10 \
  --max-attempts=10 --min-backoff=5s --max-backoff=300s \
  --max-doublings=5 --max-retry-duration=3600s

for SECRET in "${DATABASE_SECRET}" "${SMTP_PASSWORD_SECRET}"; do
  gcloud secrets add-iam-policy-binding "${SECRET}" \
    --member="serviceAccount:${WORKER_SERVICE_ACCOUNT}" \
    --role=roles/secretmanager.secretAccessor --quiet
done
gcloud projects add-iam-policy-binding "${PROJECT}" \
  --member="serviceAccount:${WORKER_SERVICE_ACCOUNT}" \
  --role=roles/cloudsql.client --condition=None --quiet

gcloud run deploy "${WORKER_SERVICE}" --image="${IMAGE}" --region="${REGION}" \
  --service-account="${WORKER_SERVICE_ACCOUNT}" --no-allow-unauthenticated \
  "${NETWORK_ARGS[@]}" \
  --args=serve,email-outbox --min=0 --max=5 --concurrency=10 --timeout=60s \
  --set-env-vars="APP_ENV=production,DB_SCHEMA=commerce,SMTP_HOST=${SMTP_HOST},SMTP_PORT=${SMTP_PORT},SMTP_USERNAME=${SMTP_USERNAME},SMTP_FROM=${SMTP_FROM},SMTP_TLS_MODE=starttls,JOB_TIMEOUT=60s,DB_MAX_CONNS=5,DB_MIN_CONNS=0" \
  --set-secrets="DATABASE_URL=${DATABASE_SECRET}:latest,SMTP_PASSWORD=${SMTP_PASSWORD_SECRET}:latest" \
  --quiet

WORKER_URL="$(gcloud run services describe "${WORKER_SERVICE}" \
  --region="${REGION}" --format='value(status.url)')"
gcloud run services add-iam-policy-binding "${WORKER_SERVICE}" \
  --region="${REGION}" --member="serviceAccount:${TASK_SERVICE_ACCOUNT}" \
  --role=roles/run.invoker --quiet
gcloud tasks queues add-iam-policy-binding "${QUEUE}" --location="${REGION}" \
  --member="serviceAccount:${API_RUNTIME_SERVICE_ACCOUNT}" \
  --role=roles/cloudtasks.enqueuer --quiet
gcloud iam service-accounts add-iam-policy-binding "${TASK_SERVICE_ACCOUNT}" \
  --member="serviceAccount:${API_RUNTIME_SERVICE_ACCOUNT}" \
  --role=roles/iam.serviceAccountUser --quiet

gcloud run services update "${API_SERVICE}" --region="${REGION}" \
  --update-env-vars="EMAIL_TASKS_ENABLED=true,EMAIL_TASKS_PROJECT=${PROJECT},EMAIL_TASKS_LOCATION=${REGION},EMAIL_TASKS_QUEUE=${QUEUE},EMAIL_TASKS_WORKER_URL=${WORKER_URL},EMAIL_TASKS_SERVICE_ACCOUNT=${TASK_SERVICE_ACCOUNT},EMAIL_TASKS_AUDIENCE=${WORKER_URL},EMAIL_TASKS_DISPATCH_TIMEOUT=2s" \
  --quiet

gcloud scheduler jobs update http "${FALLBACK_SCHEDULER}" \
  --location="${REGION}" --schedule='*/30 * * * *' --quiet

test "$(gcloud run services describe "${WORKER_SERVICE}" --region="${REGION}" \
  --format='value(status.conditions[0].status)')" = "True"
test -z "$(gcloud run services get-iam-policy "${WORKER_SERVICE}" --region="${REGION}" \
  --flatten='bindings[].members' --filter='bindings.members:allUsers' \
  --format='value(bindings.members)')"
test "$(gcloud tasks queues describe "${QUEUE}" --location="${REGION}" \
  --format='value(state)')" = "RUNNING"
gcloud run jobs describe "${FALLBACK_JOB}" --region="${REGION}" >/dev/null

printf 'Email Tasks ready. Worker: %s\n' "${WORKER_URL}"
