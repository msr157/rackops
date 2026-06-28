# Branch Strategy — RackOps

## Overview

RackOps follows a **trunk-based GitOps workflow** with three long-lived branches. Each branch maps to a specific environment. Only controlled, reviewed code reaches production.

---

## Branches

### `main` — Production

- **Purpose**: Stable, production-ready code.
- **Protected**: Yes — direct pushes are discouraged; changes arrive via merged PRs from `qa`.
- **Triggers**: Every push to `main` triggers:
  1. GitHub Action mirrors the repo to Gitea.
  2. Gitea CI builds, validates, and runs health checks.
  3. If all checks pass → Dokploy deploys to **production**.

### `qa` — Quality Assurance / Staging

- **Purpose**: Pre-production testing and acceptance.
- **Triggers**: Can be wired to deploy to a staging environment (optional).
- **Flow**: Features are merged from `development` into `qa` for testing.

### `development` — Active Development

- **Purpose**: Day-to-day feature work, bug fixes, experiments.
- **Convention**: Feature branches (`feat/xxx`, `fix/xxx`) are created from `development` and merged back via PR.
- **No auto-deploy**: Pushing to `development` does **not** trigger any CI/CD.

---

## Workflow Diagram

```
developer/feature-branch
        │
        │  Pull Request
        ▼
   development  ──────→  (local dev server)
        │
        │  Pull Request + Review
        ▼
       qa  ───────────→  (staging environment)
        │
        │  Pull Request + Approval
        ▼
      main  ──────────→  GitHub Action  →  Gitea CI  →  Dokploy Production
```

---

## Rules

| Rule | Applies to |
|---|---|
| All changes via Pull Requests | `qa`, `main` |
| At least 1 approval required before merge | `main` |
| CI must pass before merge | `main` |
| Direct force-push forbidden | `main` |
| Auto-deployment | `main` only |

---

## Commit Convention

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add rack auto-provisioning endpoint
fix:  correct health check response format
docs: update branch strategy documentation
ci:   increase docker build cache timeout
chore: bump nginx to 1.27
```

---

## Release Process

1. Merge `development` → `qa` via PR.
2. Run QA and acceptance tests on staging.
3. Merge `qa` → `main` via PR with approval.
4. CI runs automatically — Dokploy deploys on success.
5. Tag the release: `git tag -a v1.x.x -m "Release v1.x.x"`.

---

## Hotfix Process

For urgent production fixes:

```bash
git checkout main
git checkout -b hotfix/critical-issue
# Make the fix
git push origin hotfix/critical-issue
# Open PR directly to main
# Merge after review → auto-deploy triggers
# Backport to development
git checkout development
git merge main
```
