import { strict as assert } from "node:assert";
import { test } from "node:test";

import { CHAIN_QUEUED_TITLE, chainRootTitle } from "./chains";
import type { DependentChain } from "./api";

const chain = (over: Partial<DependentChain> = {}): DependentChain =>
  ({
    projectId: "p",
    rootFeatureId: "root",
    queued: ["a", "b"],
    pausedAtRequest: false,
    ...over,
  }) as DependentChain;

test("chainRootTitle: names the features queued behind the root", () => {
  const t = chainRootTitle(chain());
  assert.match(t, /2 queued dependent features: a, b/);
});

test("chainRootTitle: singular for one queued feature", () => {
  assert.match(chainRootTitle(chain({ queued: ["a"] })), /1 queued dependent feature: a/);
});

// The server omits an empty `queued`, and an older build could send null.
// Reading .length on that used to crash the whole feature row rather than
// degrade to "nothing queued".
test("chainRootTitle: a missing queue degrades, it does not throw", () => {
  assert.match(
    chainRootTitle(chain({ queued: undefined })),
    /nothing queued behind it/,
  );
  assert.match(chainRootTitle(chain({ queued: [] })), /nothing queued behind it/);
  assert.equal(chainRootTitle(undefined), "Running with dependents");
});

// An external wait is the difference between "waiting its turn" and "never
// going to run" — it must not be left to inference.
test("chainRootTitle: an external wait is called out as a stall", () => {
  const t = chainRootTitle(chain({ waitsOnExternal: ["other-feat"] }));
  assert.match(t, /Stalls on other-feat/);
});

// A chain requested while the project was ALREADY paused keeps draining;
// one requested while it was running does not. Only the second case gets
// the warning, or it would tell the user the opposite of the truth.
test("chainRootTitle: the pause warning tracks pausedAtRequest", () => {
  assert.match(chainRootTitle(chain()), /Pausing the project will hold this chain/);
  assert.doesNotMatch(
    chainRootTitle(chain({ pausedAtRequest: true })),
    /Pausing the project will hold/,
  );
});

test("CHAIN_QUEUED_TITLE explains that no second click is needed", () => {
  assert.match(CHAIN_QUEUED_TITLE, /no second click/);
});
