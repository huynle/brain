import test from "node:test";
import assert from "node:assert/strict";
import { readerHref, readerLink, remarkReaderWikiLinks } from "./readerLinks";
const path = "projects/demo/scratch/aaaaaaaa.md";
for (const origin of ["http://127.0.0.1:3333", "https://brain.huynle.com"]) {
  test(`reader links preserve origin, path, and heading at ${origin}`, () => {
    assert.equal(
      readerLink("../plan/custom-name.md#details", path, origin),
      readerHref("projects/demo/plan/custom-name.md", "details"),
    );
    assert.equal(
      readerLink("projects/demo/scratch/bbbbbbbb.md", path, origin),
      readerHref("projects/demo/scratch/bbbbbbbb.md"),
    );
    assert.equal(
      readerLink(origin + "/?entry=bbbbbbbb#details", path, origin),
      readerHref("bbbbbbbb", "details"),
    );
    assert.equal(
      readerLink(origin + "/projects/demo/scratch/bbbbbbbb.md", path, origin),
      readerHref("projects/demo/scratch/bbbbbbbb.md"),
    );
    assert.equal(readerLink("bbbbbbbb", path, origin), readerHref("bbbbbbbb"));
    assert.equal(readerLink("#details", path, origin), "#details");
    assert.equal(
      readerLink("https://example.com/doc", path, origin),
      undefined,
    );
    assert.equal(readerLink("javascript:alert(1)", path, origin), undefined);
  });
}
test("wiki links retain aliases and skip inline code and existing links", () => {
  const tree: any = {
    type: "root",
    children: [
      {
        type: "paragraph",
        children: [
          {
            type: "text",
            value: "See [[bbbbbbbb|B]] and [[projects/p/scratch/a.md]].",
          },
          { type: "inlineCode", value: "[[literal]]" },
          {
            type: "link",
            url: "https://example.com",
            children: [{ type: "text", value: "[[literal]]" }],
          },
        ],
      },
    ],
  };
  remarkReaderWikiLinks()(tree);
  const children = tree.children[0].children;
  assert.equal(children[1].type, "link");
  assert.equal(children[1].url, "bbbbbbbb");
  assert.equal(children[1].children?.[0].value, "B");
  assert.equal(children[5].type, "inlineCode");
  assert.equal(children[5].value, "[[literal]]");
  assert.equal(children[6].children?.[0].value, "[[literal]]");
});
