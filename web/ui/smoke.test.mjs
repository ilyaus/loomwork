import test from "node:test";
import assert from "node:assert/strict";
import {spawn} from "node:child_process";
import {mkdir, mkdtemp, readFile, writeFile} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {fileURLToPath} from "node:url";
import puppeteer from "puppeteer-core";

const binary = fileURLToPath(new URL("../../bin/loomwork", import.meta.url));

test("Explorer creation, reports, project import, and local Markdown versioning", {timeout: 90000}, async () => {
  const home = await mkdtemp(join(tmpdir(), "loomwork-ui-test-"));
  const server = spawn(binary, ["serve", "--home", home, "--addr", "127.0.0.1:0"]);
  let browser;
  try {
    const base = await new Promise((resolve, reject) => {
      let output = "";
      const timer = setTimeout(() => reject(new Error("Server did not start")), 10000);
      const read = data => {
        output += data;
        const match = output.match(/http:\/\/127\.0\.0\.1:\d+/);
        if (match) {clearTimeout(timer); resolve(match[0]);}
      };
      server.stdout.on("data", read);
      server.stderr.on("data", read);
      server.on("error", reject);
      server.on("exit", code => {clearTimeout(timer); reject(new Error(`Server exited: ${code} ${output}`));});
    });
    const api = async (path, body) => {
      const response = await fetch(`${base}/api${path}`, body ? {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify(body)} : {});
      assert.equal(response.ok, true, await response.clone().text());
      return response.json();
    };
    const project = await api("/projects", {name: "Browser regression"});
    const requirement = await api(`/projects/${project.id}/requirements`, {text: "An order can be retrieved"});
    await api(`/projects/${project.id}/test-suites`, {suite_id: "orders", title: "Order tests", cases: [{name: "Retrieve an order", requirement_ids: [requirement.id], scenario: "happy-path", request: {method: "GET", path: "/orders/1"}, expected: {status: 200}}]});
    browser = await puppeteer.launch({executablePath: process.env.CHROME_BIN || "/usr/bin/google-chrome", headless: true, args: ["--no-sandbox"]});
    const page = await browser.newPage();
    await page.setViewport({width: 1440, height: 1000});
    const errors = [];
    page.on("pageerror", error => errors.push(error.message));
    const fill = async (label, value) => {
      const input = await page.evaluateHandle(label => [...document.querySelectorAll("dialog[open] .field")].find(field => field.querySelector(".field-label")?.textContent === label)?.querySelector("input,textarea"), label);
      assert.ok(input.asElement(), `Missing ${label}`);
      await input.asElement().click();
      await page.keyboard.down("Control");
      await page.keyboard.press("A");
      await page.keyboard.up("Control");
      await page.keyboard.press("Backspace");
      await input.asElement().type(value);
      await input.dispose();
    };
    const clickText = async (selector, text) => {
      await page.evaluate((selector, text) => [...document.querySelectorAll(selector)].find(element => element.textContent.includes(text) || element.getAttribute("aria-label") === text).click(), selector, text);
    };
    await page.goto(`${base}/projects/${project.id}`);
    await page.waitForSelector(".tree-group-label");
    const reqGroup = await page.evaluateHandle(() => [...document.querySelectorAll(".tree-group")].find(g => g.querySelector(".tree-group-label")?.textContent.includes("Requirements")));
    assert.equal(await page.evaluate(g => g.classList.contains("open"), reqGroup), false);
    await clickText(".tree-group-label", "Requirements");
    assert.equal(await page.evaluate(g => g.classList.contains("open"), reqGroup), true);
    await page.waitForSelector(".tree-pane .tree-node");
    await clickText(".tree-group-label", "Requirements");
    assert.equal(await page.evaluate(g => g.classList.contains("open"), reqGroup), false);
    for (const [label, fields, family] of [
      ["Artifacts", {Name: "notes.md", Content: "# Notes\nOriginal content"}, "artifacts"],
      ["Agent definitions", {"Agent name": "browser-agent"}, "agent-definitions"],
      ["Override rules", {"Rule id": "browser-rule", Title: "Missing records", Rationale: "Missing collections are empty."}, "override-rules"],
      ["Reports", {Name: "suite/v1/run.json", Content: '{"summary":{"total":3,"passed":2,"failed":1}}'}, "reports"],
    ]) {
      await clickText(".tree-group-label", label);
      await page.waitForSelector(".list-head .btn.primary");
      await page.click(".list-head .btn.primary");
      await page.waitForSelector("dialog[open]");
      for (const [field, value] of Object.entries(fields)) await fill(field, value);
      await page.click("dialog[open] button[type=submit]");
      await page.waitForFunction(() => !document.querySelector("dialog[open]"));
      const entities = await api(`/projects/${project.id}/${family}`);
      assert.equal(entities.length, 1, family);
      await page.waitForSelector(".item-viewer");
    }
    await page.click('[aria-label="Add report"]');
    await fill("Name", "suite/v1/run.json");
    await fill("Content", "{}");
    await page.click("dialog[open] button[type=submit]");
    await page.waitForSelector("dialog[open] [role=alert]");
    assert.match(await page.$eval("dialog[open] [role=alert]", element => element.textContent), /already exists/);
    await clickText("dialog[open] button", "Cancel");
    await page.click('[aria-label="Add report"]');
    assert.equal(await page.$("dialog[open] [role=alert]"), null);
    const reportPath = join(home, "uploaded.json");
    await writeFile(reportPath, '{"summary":{"total":1,"passed":1,"failed":0}}');
    const fileInput = await page.$('dialog[open] input[type=file]');
    await fileInput.uploadFile(reportPath);
    await page.waitForFunction(() => document.querySelector('dialog[open] input:not([type])')?.value === "uploaded.json");
    await page.click("dialog[open] button[type=submit]");
    await page.waitForFunction(() => !document.querySelector("dialog[open]"));
    assert.equal((await api(`/projects/${project.id}/reports`)).length, 2);
    const health = await api(`/projects/${project.id}/testability`);
    assert.equal(health.reports, 2);
    assert.ok(health.lastRun);
    await clickText(".tree-group-label", "Requirements");
    await page.waitForSelector(".req-row .requirement-test-link");
    assert.match(await page.$eval(".req-row .requirement-tests", element => element.textContent), /Retrieve an order/);
    await page.click(".req-row .requirement-test-link");
    await page.waitForSelector(".requirement-test-preview .case-card");
    assert.match(await page.$eval(".requirement-summary .req-quote", element => element.textContent), /An order can be retrieved/);
    assert.match(await page.$eval(".requirement-test-preview", element => element.textContent), /Retrieve an order/);
    await clickText(".requirement-summary button", "Back to requirements");
    await page.waitForSelector(".req-id");
    await page.click(".req-id");
    await page.waitForSelector(".requirement-test-preview .case-card");
    await page.setViewport({width: 860, height: 740});
    await clickText(".tree-group-label", "Artifacts");
    await page.waitForSelector(".list-head .btn.primary");
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    const source = join(home, "source");
    const scenarioName = "specs/001-example/sdd-qa/run/scenarios/api/FR-001.api.md";
    const sourceText = "---\nfr: FR-001\nartifact: api-test-scenarios\n---\n# Scenario\nGiven an order, expect it to be returned.\n";
    const secondScenario = "specs/001-example/sdd-qa/run/generated-scenarios/api/tests/POST-orders.md";
    for (const [name, text] of Object.entries({
      ".specify/memory/constitution.md": "# Principles\nKeep things simple.",
      "specs/001-example/spec.md": "# Feature Specification: Orders\n- **FR-001**: Return orders\n- **SC-001**: Measurable result\n",
      [scenarioName]: sourceText,
      [secondScenario]: "# Create an order\n**Purpose:** FR-001\nVerify the POST response.\n\n" + "A detailed test step.\n\n".repeat(40),
      "specs/002-other/spec.md": "# Other\n- **FR-001**: Other requirement\n",
    })) {
      await mkdir(join(source, name, ".."), {recursive: true});
      await writeFile(join(source, name), text);
    }
    await page.setViewport({width: 1440, height: 1000});
    await page.goto(base);
    await page.waitForSelector(".landing-actions");
    await clickText(".landing-actions button", "Import project");
    await fill("Source directory", source);
    await page.click("dialog[open] button[type=submit]");
    await page.waitForSelector(".import-feature");
    assert.equal((await page.$$(".import-feature")).length, 2);
    assert.equal((await api("/projects")).length, 1);
    await page.click(".import-feature:last-child input");
    await fill("Local project name", "Imported orders");
    await page.click("dialog[open] button[type=submit]");
    await page.waitForSelector(".import-notice");
    const imported = (await api("/projects")).find(project => project.name === "Imported orders");
    const importedProject = await api(`/projects/${imported.id}`);
    assert.deepEqual(importedProject.import.features, ["001-example"]);
    await clickText(".tree-group-label", "Artifacts");
    await page.waitForSelector(".list-head");
    assert.ok(await page.$('.list-view .file-tree'), "Artifacts should have nested folder navigation");
    const folder = '.list-view .file-folder-button[data-path="specs/001-example"]';
    assert.equal(await page.$eval(folder, element => element.getAttribute("aria-expanded")), "false");
    await page.click(folder);
    assert.ok(await page.$('.list-view .file-entry[title="specs/001-example/spec.md"]'));
    await page.click(folder);
    assert.equal(await page.$('.list-view .file-entry[title="specs/001-example/spec.md"]'), null);
    await page.reload();
    await page.waitForSelector(folder);
    assert.equal(await page.$eval(folder, element => element.getAttribute("aria-expanded")), "false");
    const explorerFolder = '.tree-pane .file-folder-button[data-path="specs/001-example"]';
    await page.click(explorerFolder);
    await page.waitForSelector('.tree-pane .file-entry[title="specs/001-example/spec.md"]');
    await page.click(explorerFolder);
    assert.equal(await page.$('.tree-pane .file-entry[title="specs/001-example/spec.md"]'), null);
    await page.type('.list-view .search input', 'spec.md');
    await page.waitForSelector('.list-view .link-row');
    await page.$eval('.list-view .search input', element => { const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set; setter.call(element, ""); element.dispatchEvent(new Event("input", {bubbles: true})); });
    await page.waitForSelector(folder);
    assert.equal(await page.$eval(folder, element => element.getAttribute("aria-expanded")), "false");
    const importedRequirements = await api(`/projects/${imported.id}/requirements`);
    assert.deepEqual(importedRequirements.map(requirement => requirement.id), ["001-FR-001", "001-SC-001"]);
    const initialTree = await api(`/projects/${imported.id}/items`);
    assert.equal(initialTree.groups.find(group => group.family === "test-suites").items[0].artifactType, "document-suite");
    assert.equal(initialTree.groups.find(group => group.family === "artifacts").items.some(item => item.name.includes("sdd-qa")), false);
    await page.waitForSelector(".tree-suite-head .tree-node");
    assert.equal(await page.$eval(".tree-suite-head .tree-toggle", el => el.getAttribute("aria-expanded")), "false");
    await page.click(".tree-suite-head .tree-node");
    await page.waitForSelector(".tree-suite-head + .file-tree");
    assert.equal(await page.$eval(".tree-suite-head .tree-toggle", el => el.getAttribute("aria-expanded")), "true");
    await page.click(".tree-suite-head .tree-node");
    assert.equal(await page.$(".tree-suite-head + .file-tree"), null);
    await clickText(".tree-group-label", "Test suites");
    await page.waitForSelector(".suite-card");
    assert.match(await page.$eval(".suite-card", element => element.textContent), /Markdown suite/);
    await clickText(".list-head button", "Configure test folders");
    await page.waitForFunction(() => document.querySelector("dialog[open] textarea")?.value.includes("sdd-qa"));
    await fill("Test folder roots", "");
    await page.click("dialog[open] button[type=submit]");
    await page.waitForFunction(() => !document.querySelector("dialog[open]"));
    assert.deepEqual((await api(`/projects/${imported.id}/test-document-settings`)).roots, []);
    const ungrouped = await api(`/projects/${imported.id}/items`);
    assert.equal(ungrouped.groups.find(group => group.family === "artifacts").items.some(item => item.name === scenarioName), true);
    await clickText(".list-head button", "Configure test folders");
    await page.waitForSelector("dialog[open]");
    await clickText("dialog[open] button", "Use import defaults");
    await page.click("dialog[open] button[type=submit]");
    await page.waitForFunction(() => !document.querySelector("dialog[open]"));
    await page.waitForSelector(".suite-card");
    await clickText(".suite-head button", "Open");
    await page.waitForFunction(() => document.querySelector(".item-family")?.textContent === "Document suites");
    await page.waitForSelector(".item-body .file-tree");
    await clickText(".item-body button", "Back to test suites");
    await page.waitForSelector(".suite-card");
    await clickText(".tree-group-label", "Requirements");
    await page.waitForSelector(".req-row");
    assert.equal((await page.$$(".req-row")).length, 2);
    assert.equal(await page.$('[aria-label="New requirement"]'), null);
    assert.equal(await page.$(".req-row .switch"), null);
    await page.waitForSelector(".req-row .requirement-test-link");
    assert.equal(await page.$eval(".req-id", element => element.textContent), "001-FR-001");
    assert.match(await page.$eval(".req-row:last-child .requirement-tests", element => element.textContent), /No linked tests/);
    await page.type('.list-view .search input', '001-FR-001');
    await page.click(`.req-row .requirement-test-link[title="${scenarioName}"]`);
    await page.waitForSelector(".requirement-test-preview .markdown");
    assert.equal(await page.$eval(".req-quote", element => element.textContent), "Return orders");
    assert.match(await page.$eval(".requirement-test-preview", element => element.textContent), /Given an order/);
    assert.equal(await page.$eval(".markdown-frontmatter", element => element.open), false);
    assert.match(await page.$eval(".markdown-frontmatter pre", element => element.textContent), /fr: FR-001/);
    const tabCount = (await page.$$(".tab")).length;
    await page.select('[aria-label="Linked test"]', `artifacts:${secondScenario}`);
    await page.waitForFunction(() => document.querySelector(".requirement-test-preview")?.textContent.includes("Verify the POST response"));
    assert.equal((await page.$$(".tab")).length, tabCount);
    await page.$eval('.viewer > .item-viewer > .item-body', element => { element.scrollTop = 600; });
    await page.waitForFunction(() => document.querySelector('.viewer > .item-viewer > .item-body')?.scrollTop > 0);
    assert.equal(await page.evaluate(() => {
      const quote = document.querySelector('.requirement-summary .req-quote').getBoundingClientRect();
      const viewport = document.querySelector('.viewer > .item-viewer > .item-body').getBoundingClientRect();
      return quote.top >= viewport.top && quote.bottom <= viewport.bottom;
    }), true, 'Requirement text stays visible while scrolling a long test');
    assert.equal(await page.$eval(".req-quote", element => element.textContent), "Return orders");
    await clickText(".requirement-summary button", "Back to requirements");
    await page.waitForSelector(".req-id");
    assert.equal(await page.$eval('.list-view .search input', element => element.value), '001-FR-001');
    assert.equal((await page.$$(".req-row")).length, 1);
    await page.click(`.req-row .requirement-test-link[title="${scenarioName}"]`);
    await page.waitForSelector(".requirement-test-preview .markdown");
    assert.equal(await page.$(".requirement-summary .entity-actions"), null);
    const localTest = importedProject.artifacts.find(artifact => artifact.name === scenarioName);
    await clickText(".requirement-test-toolbar button", "Open test separately");
    await page.waitForSelector(".item-head .btn");
    await clickText(".item-head .btn", "New version");
    await page.waitForSelector("dialog[open]");
    assert.equal(await page.$eval("dialog[open] textarea", element => element.value), sourceText);
    await fill("Content", "# Revised scenario\nKeep the local test up to date.");
    await page.click("dialog[open] button[type=submit]");
    await page.waitForFunction(() => !document.querySelector("dialog[open]"));
    await page.waitForFunction(() => document.querySelector('.version-switcher select')?.value === "2");
    assert.equal(await readFile(join(source, scenarioName), "utf8"), sourceText);
    const revisions = await api(`/projects/${imported.id}/items/artifacts/${localTest.id}/history`);
    assert.deepEqual(revisions.map(revision => revision.version), [2, 1]);
    await page.select(".version-switcher select", "1");
    await page.waitForFunction(() => document.querySelector(".notice")?.textContent.includes("Viewing version 1"));
    assert.equal(await page.$(".item-head .btn"), null);
    assert.match(await page.$eval(".item-body", element => element.textContent), /Given an order/);
    await clickText(".notice button", "Back to current");
    await page.waitForSelector(".item-head .btn");
    assert.deepEqual(errors, []);
  } finally {
    await browser?.close();
    server.kill("SIGTERM");
  }
});
