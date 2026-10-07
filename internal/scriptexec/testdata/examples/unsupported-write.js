// No V1 write has an approved service preflight binding here.
// Catching refusal does not create a plan, receipt, or committed object.
let code;
try {
  await brain.entries.create({type: "note", title: "Example", content: "Example"});
} catch (error) {
  code = error.code;
}
({code});
