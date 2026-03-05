/**
 * Tests for Project Filter
 */

import {
  describe,
  it,
  expect,
  beforeEach,
  afterEach,
  mock,
} from "bun:test";
import {
  matchesGlob,
  filterProjects,
  resolveProjects,
  type ProjectFilter,
} from "./project-filter";
import { resetConfig } from "./config";

describe("project-filter", () => {
  beforeEach(() => {
    resetConfig();
  });

  afterEach(() => {
    resetConfig();
  });

  // ===========================================================================
  // matchesGlob tests
  // ===========================================================================

  describe("matchesGlob", () => {
    describe("exact match", () => {
      it("matches identical strings", () => {
        expect(matchesGlob("my-project", "my-project")).toBe(true);
      });

      it("does not match different strings", () => {
        expect(matchesGlob("my-project", "other-project")).toBe(false);
      });

      it("is case-sensitive", () => {
        expect(matchesGlob("My-Project", "my-project")).toBe(false);
      });
    });

    describe("wildcard * (any chars)", () => {
      it("matches prefix pattern", () => {
        expect(matchesGlob("test-api", "test-*")).toBe(true);
        expect(matchesGlob("test-web", "test-*")).toBe(true);
        expect(matchesGlob("test-", "test-*")).toBe(true);
      });

      it("matches suffix pattern", () => {
        expect(matchesGlob("my-project", "*-project")).toBe(true);
        expect(matchesGlob("your-project", "*-project")).toBe(true);
      });

      it("matches middle pattern", () => {
        expect(matchesGlob("my-test-api", "*-test-*")).toBe(true);
        expect(matchesGlob("your-test-web", "*-test-*")).toBe(true);
      });

      it("matches multiple wildcards", () => {
        expect(matchesGlob("a-b-c", "*-*-*")).toBe(true);
        expect(matchesGlob("foo-bar-baz", "*-*-*")).toBe(true);
      });

      it("matches empty with wildcard", () => {
        expect(matchesGlob("test", "test*")).toBe(true);
        expect(matchesGlob("", "*")).toBe(true);
      });

      it("does not match when pattern doesn't align", () => {
        expect(matchesGlob("prod-api", "test-*")).toBe(false);
        expect(matchesGlob("my-service", "*-project")).toBe(false);
      });
    });

    describe("wildcard ? (single char)", () => {
      it("matches single character", () => {
        expect(matchesGlob("test-a", "test-?")).toBe(true);
        expect(matchesGlob("test-b", "test-?")).toBe(true);
      });

      it("does not match multiple characters", () => {
        expect(matchesGlob("test-ab", "test-?")).toBe(false);
      });

      it("does not match empty", () => {
        expect(matchesGlob("test-", "test-?")).toBe(false);
      });

      it("matches multiple single char wildcards", () => {
        expect(matchesGlob("abc", "???")).toBe(true);
        expect(matchesGlob("ab", "???")).toBe(false);
        expect(matchesGlob("abcd", "???")).toBe(false);
      });
    });

    describe("combined wildcards", () => {
      it("handles * and ? together", () => {
        expect(matchesGlob("test-v1-api", "test-v?-*")).toBe(true);
        expect(matchesGlob("test-v2-web", "test-v?-*")).toBe(true);
        expect(matchesGlob("test-v12-api", "test-v?-*")).toBe(false);
      });

      it("handles complex patterns", () => {
        expect(matchesGlob("prod-api-v1", "prod-*-v?")).toBe(true);
        expect(matchesGlob("prod-service-v2", "prod-*-v?")).toBe(true);
      });
    });

    describe("special regex characters", () => {
      it("escapes dots", () => {
        expect(matchesGlob("test.api", "test.api")).toBe(true);
        expect(matchesGlob("testXapi", "test.api")).toBe(false);
      });

      it("escapes other special chars", () => {
        expect(matchesGlob("test[1]", "test[1]")).toBe(true);
        expect(matchesGlob("test(1)", "test(1)")).toBe(true);
        expect(matchesGlob("test+1", "test+1")).toBe(true);
      });
    });

    describe("edge cases", () => {
      it("handles empty project name", () => {
        expect(matchesGlob("", "")).toBe(true);
        expect(matchesGlob("", "*")).toBe(true);
        expect(matchesGlob("", "?")).toBe(false);
      });

      it("handles empty pattern", () => {
        expect(matchesGlob("test", "")).toBe(false);
        expect(matchesGlob("", "")).toBe(true);
      });
    });
  });

  // ===========================================================================
  // filterProjects tests
  // ===========================================================================

  describe("filterProjects", () => {
    const allProjects = [
      "brain-api",
      "brain-web",
      "legacy-system",
      "prod-api",
      "prod-web",
      "test-api",
      "test-web",
    ];

    describe("includes only", () => {
      it("filters to matching projects", () => {
        const filter: ProjectFilter = {
          includes: ["prod-*"],
          excludes: [],
        };

        const result = filterProjects(allProjects, filter);

        expect(result).toEqual(["prod-api", "prod-web"]);
      });

      it("handles multiple include patterns", () => {
        const filter: ProjectFilter = {
          includes: ["brain-*", "prod-*"],
          excludes: [],
        };

        const result = filterProjects(allProjects, filter);

        expect(result).toEqual([
          "brain-api",
          "brain-web",
          "prod-api",
          "prod-web",
        ]);
      });

      it("returns empty when no matches", () => {
        const filter: ProjectFilter = {
          includes: ["nonexistent-*"],
          excludes: [],
        };

        const result = filterProjects(allProjects, filter);

        expect(result).toEqual([]);
      });
    });

    describe("excludes only", () => {
      it("removes matching projects", () => {
        const filter: ProjectFilter = {
          includes: [],
          excludes: ["test-*"],
        };

        const result = filterProjects(allProjects, filter);

        expect(result).toEqual([
          "brain-api",
          "brain-web",
          "legacy-system",
          "prod-api",
          "prod-web",
        ]);
      });

      it("handles multiple exclude patterns", () => {
        const filter: ProjectFilter = {
          includes: [],
          excludes: ["test-*", "legacy-*"],
        };

        const result = filterProjects(allProjects, filter);

        expect(result).toEqual([
          "brain-api",
          "brain-web",
          "prod-api",
          "prod-web",
        ]);
      });

      it("returns all when no matches", () => {
        const filter: ProjectFilter = {
          includes: [],
          excludes: ["nonexistent-*"],
        };

        const result = filterProjects(allProjects, filter);

        expect(result).toEqual(allProjects.sort());
      });
    });

    describe("includes and excludes combined", () => {
      it("applies includes first, then excludes", () => {
        const filter: ProjectFilter = {
          includes: ["*-api", "*-web"],
          excludes: ["test-*"],
        };

        const result = filterProjects(allProjects, filter);

        // Include *-api and *-web, then exclude test-*
        expect(result).toEqual([
          "brain-api",
          "brain-web",
          "prod-api",
          "prod-web",
        ]);
      });

      it("exclude can remove all included projects", () => {
        const filter: ProjectFilter = {
          includes: ["test-*"],
          excludes: ["test-*"],
        };

        const result = filterProjects(allProjects, filter);

        expect(result).toEqual([]);
      });

      it("handles overlapping patterns correctly", () => {
        const filter: ProjectFilter = {
          includes: ["prod-*", "test-*"],
          excludes: ["*-web"],
        };

        const result = filterProjects(allProjects, filter);

        expect(result).toEqual(["prod-api", "test-api"]);
      });
    });

    describe("empty patterns", () => {
      it("returns all projects sorted when both empty", () => {
        const filter: ProjectFilter = {
          includes: [],
          excludes: [],
        };

        const result = filterProjects(allProjects, filter);

        expect(result).toEqual(allProjects.sort());
      });
    });

    describe("edge cases", () => {
      it("handles empty project list", () => {
        const filter: ProjectFilter = {
          includes: ["prod-*"],
          excludes: [],
        };

        const result = filterProjects([], filter);

        expect(result).toEqual([]);
      });

      it("returns sorted results", () => {
        const unsorted = ["z-project", "a-project", "m-project"];
        const filter: ProjectFilter = {
          includes: [],
          excludes: [],
        };

        const result = filterProjects(unsorted, filter);

        expect(result).toEqual(["a-project", "m-project", "z-project"]);
      });

      it("does not modify original array", () => {
        const original = ["b", "a", "c"];
        const filter: ProjectFilter = { includes: [], excludes: [] };

        filterProjects(original, filter);

        expect(original).toEqual(["b", "a", "c"]);
      });
    });
  });

  // ===========================================================================
  // resolveProjects tests
  // ===========================================================================

  describe("resolveProjects", () => {
    let originalFetch: typeof fetch;

    // Helper to create a mock fetch function (same pattern as api-client.test.ts)
    function createMockFetch(handler: () => Promise<Response>) {
      const mockFn = mock(handler) as unknown as typeof fetch;
      return mockFn;
    }

    beforeEach(() => {
      originalFetch = globalThis.fetch;
    });

    afterEach(() => {
      globalThis.fetch = originalFetch;
    });

    it("fetches and filters projects from API", async () => {
      const mockProjects = ["brain-api", "test-api", "prod-api"];

      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(JSON.stringify({ projects: mockProjects }), {
            status: 200,
          })
        )
      );

      const filter: ProjectFilter = {
        includes: [],
        excludes: ["test-*"],
      };

      const result = await resolveProjects("http://localhost:3000", filter);

      expect(result).toEqual(["brain-api", "prod-api"]);
    });

    it("applies include filters", async () => {
      const mockProjects = ["brain-api", "brain-web", "prod-api"];

      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(JSON.stringify({ projects: mockProjects }), {
            status: 200,
          })
        )
      );

      const filter: ProjectFilter = {
        includes: ["brain-*"],
        excludes: [],
      };

      const result = await resolveProjects("http://localhost:3000", filter);

      expect(result).toEqual(["brain-api", "brain-web"]);
    });

    it("throws on API error", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(new Response("Not found", { status: 404 }))
      );

      const filter: ProjectFilter = { includes: [], excludes: [] };

      await expect(
        resolveProjects("http://localhost:3000", filter)
      ).rejects.toThrow("Failed to fetch projects: 404");
    });

    it("handles empty projects response", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(JSON.stringify({ projects: [] }), { status: 200 })
        )
      );

      const filter: ProjectFilter = { includes: [], excludes: [] };

      const result = await resolveProjects("http://localhost:3000", filter);

      expect(result).toEqual([]);
    });

    it("handles missing projects field in response", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(new Response(JSON.stringify({}), { status: 200 }))
      );

      const filter: ProjectFilter = { includes: [], excludes: [] };

      const result = await resolveProjects("http://localhost:3000", filter);

      expect(result).toEqual([]);
    });

    it("calls correct API endpoint", async () => {
      let capturedUrl: string | undefined;

      globalThis.fetch = ((url: string) => {
        capturedUrl = url;
        return Promise.resolve(
          new Response(JSON.stringify({ projects: [] }), { status: 200 })
        );
      }) as typeof fetch;

      const filter: ProjectFilter = { includes: [], excludes: [] };

      await resolveProjects("http://localhost:3000", filter);

      expect(capturedUrl).toBe("http://localhost:3000/api/v1/tasks");
    });
  });
});
