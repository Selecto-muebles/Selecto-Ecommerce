export const PROJECT = 'destry-development';
export const OWNER = 'selecto-monitoring-v1';
export const labels = key => ({ managed_by: OWNER, selecto_key: key });
const jobs = ['destry-expire-orders-staging', 'destry-email-outbox-staging', 'destry-payment-notifications-staging'];
const services = ['destry-ecommerce-staging', 'destry-admin-staging', 'destry-frontend-staging', 'destry-payments-staging', 'destry-email-outbox-worker-staging'];
const only = (label, values) => '(' + values.map(value => `${label}="${value}"`).join(' OR ') + ')';
const project = `resource.labels.project_id="${PROJECT}"`;
const jobFilter = `${project} AND resource.type="cloud_run_job" AND metric.type="run.googleapis.com/job/completed_execution_count"`;

export function uptimeConfigs() {
  return [
    ['storefront', 'selectosport.com', '/', '<title>Equipamiento Deportivo | Selecto</title>'],
    ['admin', 'admin.selectosport.com', '/login', '<title>Selecto Admin | Panel de Control</title>'],
    ['api', 'api.selectosport.com', '/ready', '"status":"ready"'],
  ].map(([key, host, path, content]) => ({
    displayName: `Selecto - disponibilidad ${key}`, userLabels: labels(key),
    monitoredResource: { type: 'uptime_url', labels: { project_id: PROJECT, host } },
    httpCheck: { requestMethod: 'GET', useSsl: true, validateSsl: true, port: 443, path, acceptedResponseStatusCodes: [{ statusValue: 200 }] },
    contentMatchers: [{ content, matcher: 'CONTAINS_STRING' }],
    period: '300s', timeout: '15s', selectedRegions: ['USA_IOWA', 'EUROPE', 'SOUTH_AMERICA'],
    checkerType: 'STATIC_IP_CHECKERS', logCheckFailures: true,
  }));
}

function threshold(displayName, filter, value, duration, aggregation, comparison = 'COMPARISON_GT') {
  return { displayName, conditionThreshold: { filter, comparison, thresholdValue: value, duration, aggregations: [aggregation], trigger: { count: 1 } } };
}
function policy(key, title, conditions, channel, instruction) {
  return {
    displayName: `Selecto - ${title}`, userLabels: labels(key), enabled: true, combiner: 'OR', conditions,
    notificationChannels: [channel], alertStrategy: { autoClose: '604800s' },
    documentation: { mimeType: 'text/markdown', content:
      `Proyecto: ${PROJECT}. Operación Selecto; los nombres históricos destry-* son intencionales.\n\n${instruction}\n\nNo borrar datos ni rotar secretos automáticamente. Runbook: ops/monitoring/README.md en Selecto-muebles/Selecto-Ecommerce.` },
  };
}

export function alertPolicies(checks, channel) {
  const policies = checks.map(check => {
    const id = check.name.split('/').at(-1);
    const filter = `${project} AND resource.type="uptime_url" AND metric.labels.check_id="${id}"`;
    return policy(`uptime-${check.userLabels.selecto_key}`, `caída o certificado ${check.userLabels.selecto_key}`, [
      threshold('Dos ubicaciones fallan durante cinco minutos', `${filter} AND metric.type="monitoring.googleapis.com/uptime_check/check_passed"`, 1, '300s', {
        alignmentPeriod: '600s', perSeriesAligner: 'ALIGN_NEXT_OLDER', crossSeriesReducer: 'REDUCE_COUNT_FALSE', groupByFields: ['resource.label.host'],
      }),
      threshold('Certificado vence en menos de 14 días', `${filter} AND metric.type="monitoring.googleapis.com/uptime_check/time_until_ssl_cert_expires"`, 14, '3600s', {
        alignmentPeriod: '3600s', perSeriesAligner: 'ALIGN_MIN', crossSeriesReducer: 'REDUCE_MIN', groupByFields: ['resource.label.host'],
      }, 'COMPARISON_LT'),
    ], channel, `Comprobar HTTPS, DNS y balanceador de ${check.monitoredResource.labels.host}. /ready también consulta PostgreSQL. No se comprueba JavaScript ni se inicia sesión.`);
  });
  policies.push(policy('http-errors', 'errores HTTP 5xx', [threshold('Cinco respuestas 5xx en cinco minutos por servicio',
    `${project} AND resource.type="cloud_run_revision" AND metric.type="run.googleapis.com/request_count" AND metric.labels.response_code_class="5xx" AND ${only('resource.labels.service_name', services)}`,
    4, '0s', { alignmentPeriod: '300s', perSeriesAligner: 'ALIGN_SUM', crossSeriesReducer: 'REDUCE_SUM', groupByFields: ['resource.label.service_name'] })], channel,
    'Revisar logs de la revisión y último despliegue. Un error aislado no dispara esta política; una caída sostenida es cubierta por uptime.'));
  policies.push(policy('job-failed', 'ejecución de job fallida', [threshold('Al menos una ejecución fallida',
    `${jobFilter} AND metric.labels.result="failed" AND ${only('resource.labels.job_name', jobs)}`, 0, '0s',
    { alignmentPeriod: '300s', perSeriesAligner: 'ALIGN_SUM', crossSeriesReducer: 'REDUCE_SUM', groupByFields: ['resource.label.job_name'] })], channel,
    'Revisar ejecución fallida y Scheduler. No reejecutar tareas de pago/correo sin comprobar idempotencia y resultado anterior.'));
  policies.push(policy('jobs-missing', 'jobs sin finalización exitosa', jobs.flatMap(job => {
    const filter = `${jobFilter} AND resource.labels.job_name="${job}" AND metric.labels.result="succeeded"`;
    const window = job.includes('email') ? '4500s' : '1200s';
    return [{
      displayName: `${job}: sin muestras durante ${job.includes('email') ? '75' : '20'} minutos`,
      conditionAbsent: { filter, duration: window, trigger: { count: 1 },
        aggregations: [{alignmentPeriod:'60s',perSeriesAligner:'ALIGN_SUM'}] },
    }, threshold(`${job}: muestras presentes pero cero éxitos`,filter,1,'60s',
      {alignmentPeriod:window,perSeriesAligner:'ALIGN_SUM'},'COMPARISON_LT')];
  }), channel, 'Revisar Scheduler pausado, permisos y ejecuciones atascadas. Expiración/pagos: cada 5 min; respaldo email: cada 30 min. Se cubren muestras ausentes y ventanas con cero éxitos. Ausencia requiere una serie observada y no garantiza detectar recursos eliminados.'));
  const operational = policy('operational-errors', 'fallos de correo o Scheduler', [{
    displayName: 'Error transaccional o del programador',
    conditionMatchedLog: { filter: `${project} AND (
      (resource.type="cloud_run_revision" AND resource.labels.service_name="destry-email-outbox-worker-staging" AND
        jsonPayload.msg=("transactional_email_failed" OR "email_outbox_retry_failed" OR "email_outbox_batch_failed")) OR
      (resource.type="cloud_run_revision" AND resource.labels.service_name="destry-ecommerce-staging" AND jsonPayload.msg="email_task_enqueue_failed") OR
      (resource.type="cloud_run_job" AND ${only('resource.labels.job_name', jobs)} AND
        jsonPayload.msg=("transactional_email_failed" OR "email_outbox_retry_failed" OR "email_outbox_batch_failed")) OR
      (resource.type="cloud_scheduler_job" AND ${only('resource.labels.job_id', jobs)} AND severity>=ERROR))` },
  }], channel, 'Revisar outbox y logs del evento. Un intento fallido puede recuperarse automáticamente; no implica entrega perdida definitiva. No incluir destinatarios ni tokens en avisos adicionales.');
  operational.alertStrategy.notificationRateLimit = { period: '1800s' };
  return [...policies, operational];
}
