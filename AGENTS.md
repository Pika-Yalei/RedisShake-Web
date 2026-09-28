# Codex project instructions

## Delivering changes

- After completing any change to this repository, run the checks relevant to that change, commit it on the current branch, and push the commit to the branch's configured remote before reporting completion. This is standing authorization for routine project pushes.
- Do not create or switch branches or use worktrees solely to perform this workflow. Never force-push.
- Keep local runtime data, credentials, secrets, and build outputs out of commits.
- If a push fails, report the commit and the specific blocker; do not claim the change is on the remote.

## UI design

For every UI change, read and follow [docs/ui-design.md](docs/ui-design.md). Keep the page title hierarchy consistent when adding management pages or changing routes.

This project targets desktop browsers only. Do not design, implement, or validate mobile layouts or touch-specific interactions unless the user explicitly requests mobile support. Existing mobile styles are not acceptance criteria and do not need separate maintenance for routine UI changes.

## Testing workflow

- Keep all test source files, helpers, fixtures, and test entry points under `tests/`. Mirror the production package paths for Go tests, such as `tests/cmd/redisshakeweb/auth_test.go`. Put package-local helpers in `*_test.go` files so the runner discovers them automatically.
- Run Go checks through the centralized overlay runner from the repository root: `go run ./tests` for all tests, `go run ./tests test ./cmd/redisshakeweb -run TestFrontendAssetsAreEmbedded` for scoped tests, and `go run ./tests vet ./...` for static analysis including tests. Use this instead of bare `go test`; see [tests/README.md](tests/README.md). Keep production package paths in command arguments.
- Run checks appropriate to the change. Documentation-only changes need a diff and link review, not a browser session or Go build. For code changes, check modified JavaScript with `node --check`, run the relevant Go tests, and build the Web binary when application code or embedded assets change. Broaden testing only when the change or a failure warrants it.
- Use Playwright in headless mode by default: `chromium.launch({ headless: true })`. Do not open a visible Chrome or Chrome for Testing window unless the user explicitly requests a visible demonstration or debugging session. If headless testing is unavailable, report the limitation instead of silently switching to a visible browser.
- Use isolated browser contexts without the user's browser profile or existing login session. Reuse the browser within a testing session; close test contexts and the browser in cleanup, including after failures. Do not repeatedly launch a browser for individual checks.
- For UI-only regression checks, intercept the relevant `/api/**` requests with deterministic fixtures, including mutation responses. Use enough sample data to exercise the changed behavior, such as pagination boundaries, empty results, and failures. Keep these checks from changing real connections, tasks, or Redis data. Use isolated test instances and data for backend or Redis integration checks.
- Exercise the affected controls through real browser input: typing, clicking, keyboard navigation, and scrolling. Assert the visible results, state preservation, and disabled controls as relevant; do not treat DOM manipulation or direct function calls as proof that the user flow works.
- For layout changes, inspect screenshots at desktop window sizes relevant to the change and check element bounds, alignment, and overflow. Do not run mobile viewport checks unless the user explicitly requests mobile support. Headless browsers can capture these screenshots; a visible window is not required. Keep screenshots, temporary fixtures, and test logs under the ignored `.redis-shake-web/dev` directory and out of commits.
- Run the real homepage and `/api/bootstrap/status` checks described below independently of mocked browser requests. Distinguish mocked UI verification, live Web/API checks, and real Redis integration tests in reports; mocked UI success does not verify a real migration.

## Development service after changes

- After every completed repository change, start or restart the development Web service as a detached background process listening on `0.0.0.0:8080`. Keep its runtime data and logs under the ignored `.redis-shake-web/dev` directory.
- Preserve active migration tasks. Check for active runs before changing the runner process; restart the Web process alone when tasks are active.
- Verify that the homepage and `/api/bootstrap/status` respond successfully before reporting completion.
- Include the localhost URL and current LAN URL(s) for active network interfaces in the final response. Discover the addresses at the time of the change; do not hardcode this machine's IP addresses in the project configuration.
- If the service cannot start or a LAN address cannot be verified, report the exact blocker instead of claiming it is available.
