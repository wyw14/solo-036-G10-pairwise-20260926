PigFlow is a local HTTP control service for long-distance pipeline pigging operations.

## Line configuration versions

The line topology (stations, segments and pressure limits) is versioned.
Operators draft a candidate version, validate it, and publish it once the
system is quiescent.

- A fresh deployment seeds an immutable, active **v1** from the compiled-in
  default topology.
- Drafts are freely editable and validated in isolation; publishing freezes
  them into an immutable revision with a monotonically increasing number and
  makes that revision active for **new** operations.
- Publishing is refused while any operation is in flight. Completed and
  cancelled runs are never rewritten: each run records the version active
  when it was created and can always be reviewed against that exact line.
- Version history is included in the persisted snapshot and restored on
  startup, so historical lookback survives restarts.

Audited writes require an operator identity, supplied either as
`"operator"` in the JSON body or the `X-Operator` header.

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/line-versions` | List drafts and published revisions |
| POST | `/api/line-versions` | Create a draft (`base_version_id` copies an existing config) |
| GET | `/api/line-versions/{versionID}` | Fetch one version |
| PUT | `/api/line-versions/{versionID}` | Replace a draft's content |
| DELETE | `/api/line-versions/{versionID}` | Abandon a draft |
| POST | `/api/line-versions/{versionID}/validate` | Validate a draft (200 valid / 422 with issues) |
| POST | `/api/line-versions/{versionID}/publish` | Freeze and activate a draft |
| GET | `/api/operations/{runID}/line-version` | The line version a run was created against |
| GET | `/api/operations?include_history=true` | All runs, including finished ones, for review |

Validation covers station/segment identity, positive length and pressure
limits, downstream ordering, segment limits not exceeding adjacent station
limits, and an enabled route from every enabled launcher to every receiver.
