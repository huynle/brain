// Async iteration has the SDK's return shape but is unavailable in this fixture.
let code;
try {
  for await (const entry of brain.entries.iterate({limit: 10})) {
    // No entry can be yielded by the disabled binding.
    throw new Error("unexpected entry");
  }
} catch (error) {
  code = error.code;
}
return {code};
