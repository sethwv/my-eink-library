import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';

import { parse } from 'yaml';

const root = path.resolve(import.meta.dirname, '../..');

test('screenshots page uses only unannotated scenarios', async () => {
  const [catalogue, showcase] = await Promise.all([
    readFile(path.join(root, 'docs/_data/screenshots.yml'), 'utf8'),
    readFile(path.join(root, 'docs/screenshots/index.md'), 'utf8'),
  ]);
  const scenarios = new Map(parse(catalogue).map((scenario) => [scenario.id, scenario]));
  const ids = [...showcase.matchAll(/screenshot-pair\.html id="([^"]+)"/g)].map((match) => match[1]);

  for (const id of ids) {
    const scenario = scenarios.get(id);
    assert.ok(scenario, `Screenshots page references unknown scenario: ${id}`);
    assert.equal(scenario.annotations?.length ?? 0, 0, `Screenshots page scenario must not have annotations: ${id}`);
  }
});
