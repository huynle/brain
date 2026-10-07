// Fixture-only: the test parent supplies {value: 20} and {value: 22}.
// There is no deployed script endpoint or authorized service transport.
const first = await brain.entries.get("first");
const second = await brain.entries.get("second", undefined);
return {sum: first.value + second.value};
