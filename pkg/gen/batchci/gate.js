// Generated inline: do not check out and execute untrusted PR code here.
const config = JSON.parse(process.env.BATCH_CI_CONFIG);
const {owner, repo} = context.repo;
const queue = context.eventName === 'merge_group';
const payload = context.payload;
const base = queue ? payload.merge_group.base_ref.replace(/^refs\/heads\//, '') : payload.pull_request.base.ref;
if (base !== config.baseBranch) throw new Error('Unexpected batch CI base branch');
const sha = queue ? payload.merge_group.head_sha : payload.pull_request.head.sha;
if (queue && (sha !== context.sha || !/^refs\/heads\/gh-readonly-queue\//.test(payload.merge_group.head_ref))) {
  throw new Error('Queue event does not identify the checked exact commit');
}
if (config.mode === 'batch') {
  const rules = await github.paginate('GET /repos/{owner}/{repo}/rules/branches/{branch}', {owner, repo, branch: base});
  const enforcedQueue = rules.some(r => r.type === 'merge_queue');
  const enforcedGate = rules.some(r => r.type === 'required_status_checks' &&
    r.parameters.required_status_checks.some(c => c.context === 'batch-ci' && c.integration_id === config.gateAppID));
  if (!enforcedQueue || !enforcedGate) throw new Error('Batch mode requires an enforced merge queue and app-bound batch-ci requirement; restore prepare mode');
}
if (!queue) {
  core.info('Fast admission only; full CI remains mandatory on the queue commit');
  return;
}
const deadline = Date.now() + 45 * 60 * 1000;
while (Date.now() < deadline) {
  const checks = await github.paginate(github.rest.checks.listForRef, {owner, repo, ref: sha, filter: 'all', per_page: 100});
  const statuses = await github.paginate(github.rest.repos.listCommitStatusesForRef, {owner, repo, ref: sha, per_page: 100});
  const runs = await github.paginate(github.rest.actions.listWorkflowRunsForRepo, {owner, repo, head_sha: sha, event: 'merge_group', per_page: 100});
  const pending = [];
  for (const required of config.results) {
    if (required.creatorID) {
      // Select newest before checking identity: a foreign replacement must
      // never reveal an older successful result from the trusted producer.
      const latest = statuses.filter(s => s.context === required.name).sort((a, b) => b.id - a.id)[0];
      if (!latest || latest.creator?.id !== required.creatorID || latest.state !== 'success') pending.push(required.name);
      continue;
    }
    const latest = checks.filter(c => c.name === required.name).sort((a, b) =>
      (b.check_suite?.id || 0) - (a.check_suite?.id || 0) || b.id - a.id)[0];
    let valid = latest && latest.head_sha === sha && latest.app?.id === required.appID &&
      latest.status === 'completed' && latest.conclusion === 'success';
    if (required.workflow) {
      // A fresh suite with no check yet must invalidate the previous suite.
      // Workflow paths, not display names, are stable identities.
      const newestRun = runs.filter(r => r.path === required.workflow && r.head_sha === sha)
        .sort((a, b) => b.id - a.id || b.run_attempt - a.run_attempt)[0];
      valid = valid && newestRun && newestRun.check_suite_id === latest.check_suite.id &&
        newestRun.status === 'completed' && newestRun.conclusion === 'success';
    }
    if (!valid) pending.push(required.name);
  }
  if (pending.length === 0) { core.info(`All required results succeeded on ${sha}`); return; }
  core.info(`Waiting on exact queue commit ${sha}: ${pending.join(', ')}`);
  await new Promise(resolve => setTimeout(resolve, 15000));
}
throw new Error('Required exact-commit batch CI did not succeed before the deadline');
