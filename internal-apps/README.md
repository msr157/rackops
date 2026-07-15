# Internal Applications

Internal tooling that supports the business but isn't shipped to customers
(CRM, admin consoles, ops dashboards, …).

Each app lives in its own directory:

```
internal-apps/
  crm/    Customer relationship management (planned)
```

When an app gains a `package.json`, add its path to `pnpm-workspace.yaml`;
when it gains a Go module, add it to `go.work`.
