const fs = require('node:fs');
const assert = require('node:assert/strict');
const script = fs.readFileSync(require('node:path').join(__dirname, 'gate.js'), 'utf8');
const AsyncFunction = Object.getPrototypeOf(async function(){}).constructor;
const gate = new AsyncFunction('github', 'context', 'core', 'process', 'Date', 'setTimeout', script);
const sha = 'a'.repeat(40);
async function attempt(change = {}) {
  const config = {mode: 'prepare', baseBranch: 'main', gateAppID: 15368,
    results: [{name: 'unit', appID: 15368, workflow: '.github/workflows/unit.yaml'}, {name: 'ci/circleci: build', creatorID: 123}]};
  const checks = [{id: 10, name: 'unit', app: {id: 15368}, head_sha: sha,
    check_suite: {id: 100}, status: 'completed', conclusion: 'success'}];
  const statuses = [{id: 11, context: 'ci/circleci: build', creator: {id: 123}, state: 'success'}];
  const runs = [{id: 20, path: '.github/workflows/unit.yaml', head_sha: sha, check_suite_id: 100,
    status: 'completed', conclusion: 'success', run_attempt: 1}];
  const rules = [{type: 'merge_queue'}, {type: 'required_status_checks', parameters: {
    required_status_checks: [{context: 'batch-ci', integration_id: 15368}]}}];
  const state = {config, checks, statuses, runs, rules};
  change.edit?.(state);
  const context = {repo: {owner: 'example', repo: 'repository'}, eventName: change.pr ? 'pull_request' : 'merge_group', sha,
    payload: {merge_group: {head_sha: change.foreignSHA ? 'b'.repeat(40) : sha, base_ref: 'refs/heads/main', head_ref: 'refs/heads/gh-readonly-queue/main/pr-1'},
      pull_request: {head: {sha}, base: {ref: 'main'}}}};
  let reads = 0;
  const github = {rest: {checks: {listForRef: 'checks'}, repos: {listCommitStatusesForRef: 'statuses'}, actions: {listWorkflowRunsForRepo: 'runs'}},
    paginate: async (api, params) => {
      reads++;
      if (typeof api === 'string' && api.startsWith('GET')) return state.rules;
      assert.equal(params.ref || params.head_sha, sha);
      return state[api];
    }};
  try {
    await gate(github, context, {info: () => {}}, {env: {BATCH_CI_CONFIG: JSON.stringify(config)}}, Date,
      () => { throw Error('pending, not successful'); });
    return {pass: true, reads};
  } catch (error) { return {pass: false, error: error.message}; }
}
module.exports = (async () => {
  assert.equal((await attempt()).pass, true);
  assert.equal((await attempt({foreignSHA: true})).pass, false);
  const admission = await attempt({pr: true});
  assert.equal(admission.pass, true); assert.equal(admission.reads, 0);
  for (const conclusion of ['skipped', 'cancelled', 'failure', 'neutral', null]) {
    assert.equal((await attempt({edit: s => { s.checks[0].conclusion = conclusion; }})).pass, false, conclusion);
  }
  for (const edit of [
    s => { s.checks.length = 0; },
    s => { s.checks[0].head_sha = 'old'; },
    s => { s.checks[0].app.id = 999; },
    s => { s.checks[0].status = 'in_progress'; },
    s => { s.runs.unshift({...s.runs[0], id: 30, check_suite_id: 200, status: 'queued'}); },
    s => { s.runs[0].run_attempt = 2; s.runs[0].status = 'in_progress'; },
    s => { s.statuses.length = 0; },
    s => { s.statuses[0].creator.id = 999; },
    s => { s.statuses.unshift({...s.statuses[0], id: 50, state: 'failure'}); },
    s => { s.checks.unshift({...s.checks[0], id: 50, check_suite: {id: 200}, app: {id: 999}}); },
    s => { s.config.mode = 'batch'; s.rules = []; },
    s => { s.config.mode = 'batch'; s.rules[1].parameters.required_status_checks[0].integration_id = 999; },
  ]) assert.equal((await attempt({edit})).pass, false, edit.toString());
  assert.equal((await attempt({edit: s => { s.config.mode = 'batch'; }})).pass, true);
  assert.equal((await attempt({pr: true, edit: s => { s.config.mode = 'batch'; s.rules = []; }})).pass, false);
})();
