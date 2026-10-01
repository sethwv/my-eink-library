// This release range predates docs/_data/screenshots.yml.
export const upTo = "v0.0.2";

export function apply(spec) {
  spec.scenarios = [
    { id: "login", actions: [{ goto: "/login" }] },
    { id: "library-grid", actions: ["login"] },
    { id: "library-dark", actions: [{ set_theme: "dark" }, "login"] },
    { id: "book-modal", actions: ["login", { open_book: "Pride and Prejudice" }] },
    { id: "admin-settings", actions: ["login", { goto: "/admin/settings" }] },
    { id: "authors", actions: ["login", { goto: "/authors" }] },
    { id: "series", actions: ["login", { goto: "/series" }] },
    { id: "admin-users", actions: ["login", { goto: "/admin/users" }] },
  ];
}
