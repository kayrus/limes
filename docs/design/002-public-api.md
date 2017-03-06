# Public API specification

## GET /domains/:domain\_id/projects
## GET /domains/:domain\_id/projects/:project\_id

Query data for projects in a domain. `:project_id` is optional for domain admins. With domain admin token, shows
projects in that token's domain. With project member permission, shows that token's project only. Arguments:

* `service`: Limit query to resources in this service (e.g. `?service=compute`). May be given multiple times.
* `resource`: When combined, with `?service=`, limit query to that resource
  (e.g. `?service=compute&resource=instances`).

Returns 200 (OK) on success. Result is a JSON document like:

```json
{
  "projects": [
    {
      "id": "8ad3bf54-2401-435e-88ad-e80fbf984c19",
      "services": [
        {
          "name": "compute",
          "resources": [
            {
              "name": "instances",
              "quota": 5,
              "usage": 1
            },
            {
              "name": "cores",
              "quota": 20,
              "usage": 2,
              "backend_quota": 50
            },
            {
              "name": "ram",
              "unit": "MiB",
              "quota": 10240,
              "usage": 2048
            }
          ],
          "scraped_at": 1486738599
        },
        {
          "name": "object_storage",
          "resources": [
            {
              "name": "capacity",
              "unit": "B",
              "quota": 1073741824,
              "usage": 104857600
            }
          ],
          "scraped_at": 1486733778
        },
        ...
      ]
    },
    ...
  ]
}
```

If `:project_id` was given, the outer key is `project` and its value is the object without the array surrounding it.

Quota/usage data for the project is ordered into `services`, then into `resources`. In the example above, services
include `compute` and `object_storage`, and the `compute` service has three resources, `instances`, `cores` and `ram`.

The data for each resource must include `quota` and `usage`. If the resource is not counted, but measured in a certain
unit, it will be given as `unit`. Clients should be prepared to handle at least the following values for `unit`:

    B     - bytes
    KiB   - kibibytes = 2^10 bytes
    MiB   - mebibytes = 2^20 bytes
    GiB   - gibibytes = 2^30 bytes
    TiB   - tebibytes = 2^40 bytes
    PiB   - pebibytes = 2^50 bytes
    EiB   - exbibytes = 2^60 bytes

Limes tracks quotas in its local database, and expects that the quota values in the backing services may only be
manipulated by the Limes service user, but not by the project's members or admins. If, nonetheless, Limes finds the
backing service to use a different quota value than what Limes expected, it will be shown in the `backend_quota` key, as
shown in the example above for the `compute/cores` resource. If a `backend_quota` value exists, a Limes client should
display a warning message to the user.

The `scraped_at` timestamp for each service denotes when Limes last checked the quota and usage values in the backing
service. The value is a standard UNIX timestamp (seconds since `1970-00-00T00:00:00Z`).

Valid values for quotas include all non-negative numbers. Backend quotas can also have the special value `-1` which
indicates an infinite or disabled quota.

TODO: Might need to add ordering and pagination to this at some point.

## GET /domains
## GET /domains/:domain\_id

Query data for domains. `:domain_id` is optional for cloud admins. With cloud admin token, shows all domains. With
domain admin token, shows that token's domain only. Arguments:

* `service`: Limit query to resources in this service. May be given multiple times.
* `resource`: When combined, with `?service=`, limit query to that resource.

Returns 200 (OK) on success. Result is a JSON document like:

```json
{
  "domains": [
    {
      "id": "d5fbe312-1f48-42ef-a36e-484659784aa0",
      "services": [
        {
          "name": "compute",
          "resources": [
            {
              "name": "instances",
              "quota": 20,
              "projects_quota": 5,
              "usage": 1
            },
            {
              "name": "cores",
              "quota": 100,
              "projects_quota": 20,
              "usage": 2,
              "backend_quota": 50
            },
            {
              "name": "ram",
              "unit": "MiB",
              "quota": 204800,
              "projects_quota": 10240,
              "usage": 2048
            }
          ],
          "max_scraped_at": 1486738599,
          "min_scraped_at": 1486728599
        },
        {
          "name": "object_storage",
          "resources": [
            {
              "name": "capacity",
              "unit": "B",
              "quota": 107374182400,
              "projects_quota": 1073741824,
              "usage": 104857600
            }
          ],
          "max_scraped_at": 1486733778,
          "min_scraped_at": 1486723778
        }
        ...
      ]
    },
    ...
  ]
}
```

If `:domain_id` was given, the outer key is `domain` and its value is the object without the array surrounding it.

Looks a lot like the project data, but each resource has two quota values: `quota` is the quota assigned by the
cloud-admin to the domain, and `projects_quota` is the sum of all quotas assigned to projects in that domain by the
domain-admin. If the backing service has a different idea of the quota values than Limes does, then `backend_quota`
shows the sum of all project quotas as seen by the backing service. If one of the backend quotas is infinite, then
the `infinite_backend_quota` key is added as for aggregated project data.

In contrast to project data, `scraped_at` is replaced by `min_scraped_at` and `max_scraped_at`, which aggregate over the
`scraped_at` timestamps of all project data for that service and domain.

If any of the aggregated backend quotas is `-1`, the `backend_quota` field will contain the sum of the
*finite* quota values only, and an additional key `infinite_backend_quota` will be added. For example:

```js
// resources before aggregation
{ "quota": 10, "usage": 0 }
{ "quota":  5, "usage": 12, "backend_quota": -1 }
{ "quota":  5, "usage": 5 }

// resources after aggregation
{ "quota": 20, "usage": 17, "backend_quota": 15, "infinite_backend_quota": true }
```

TODO: Open question: Instead of aggregating backend quotas, maybe just include
a `warnings` field that counts projects with `quota != backend_quota`?

## GET /clusters
## GET /clusters/:cluster\_id

Query data for clusters. Requires a cloud-admin token. Arguments:

* `service`: Limit query to resources in this service. May be given multiple times.
* `resource`: When combined, with `?service=`, limit query to that resource.

Returns 200 (OK) on success. Result is a JSON document like:

```json
{
  "clusters": [
    {
      "id": "example-cluster",
      "services": [
        {
          "name": "compute",
          "resources": [
            {
              "name": "instances",
              "capacity": -1,
              "domains_quota": 20,
              "usage": 1
            },
            {
              "name": "cores",
              "capacity": 1000,
              "domains_quota": 100,
              "usage": 2
            },
            {
              "name": "ram",
              "unit": "MiB",
              "capacity": 1048576,
              "domains_quota": 204800,
              "usage": 2048
            }
          ],
          "max_scraped_at": 1486738599,
          "min_scraped_at": 1486728599
        },
        {
          "name": "object_storage",
          "resources": [
            {
              "name": "capacity",
              "unit": "B",
              "capacity": 60000000000000,
              "domains_quota": 107374182400,
              "usage": 104857600
            }
          ],
          "max_scraped_at": 1486733778,
          "min_scraped_at": 1486723778
        },
        ...
      ]
    },
    ...
  ]
}
```

If `:cluster_id` was given, the outer key is `cluster` and its value is the object without the array surrounding it.

Clusters do not have a quota, but they are constrained by the `capacity` for each resource. The `domains_quota` field
behaves just like the `projects_quota` key on domain level. Discrepancies between project quotas in Limes and in backing
services will not be shown on this level, so there is no `backend_quota` key.

Like with domain data, there are `min_scraped_at` and `max_scraped_at` timestamps for each service, aggregating over all
project data in the whole cloud.

For resources belonging to a cluster-local service (the default), the reported quota and usage is aggregated only over
domains in this cluster. For resources belonging to a shared service, the reported quota and usage is aggregated over
all domains in all clusters, and will thus be the same for every cluster listed.

## POST /domains/discover

Requires a cloud-admin token. Queries Keystone in order to discover newly-created domains that Limes does not yet know
about.

When no new domains were found, returns 204 (No Content). Otherwise, returns 202 (Accepted) and a JSON document listing
the newly discovered domains:

```json
{
  "new_domains": [
    { "id": "94cfaed4-3062-47d2-9299-ef599d5ffbfb" },
    { "id": "b66dcb34-ea53-4872-b99b-123ae9c581b4" },
    ...
  ]
}
```

When the call returns, quota/usage data for these domains will not yet be available (thus return code 202).

*Rationale:* When a cloud administrator creates a new domain, he might want to assign quota to that domain immediately
after that, but he can only do so after Limes has discovered the new domain. Limes will do so automatically after some
time through scheduled auto-discovery, but this call can be used to reduce the waiting time.

## POST /domains/:domain\_id/projects/discover

Requires a domain-admin token for the specified domain. Queries Keystone in order to discover newly-created projects in
this domain that Limes does not yet know about. This works exactly like `POST /domains/discover`, except that the JSON
document will list `new_projects` instead of `new_domains`.

*Rationale:* Same as for domain discovery: The domain admin might want to assign quotas immediately after creating a new
project.

## POST /domains/:domain\_id/projects/:project\_id/sync

Requires a project-admin token for the specified project. Schedules a sync job that pulls quota and usage data for this
project from the backing services into Limes' local database. When the job was scheduled successfully, returns 202
(Accepted).

If the project does not exist in Limes' database yet, query Keystone to see if this project was just created. If so, create the project in Limes' database before returning 202 (Accepted).

*Rationale:* When a project administrator wants to adjust her project's quotas, she might discover that the usage data
shown by Limes is out-of-date. She can then use this call to refresh the usage data in order to make a more informed
decision about how to adjust her quotas.

## PUT /domains/:domain\_id

Set quotas for the given domain. Requires a cloud-admin token, and a request body that is a JSON document like:

```json
{
  "domain": {
    "services": [
      {
        "name": "compute",
        "resources": [
          {
            "name": "instances",
            "quota": 30
          },
          {
            "name": "cores",
            "quota": 150
          }
        ]
      },
      {
        "name": "object_storage",
        "resources": [
          {
            "name": "capacity",
            "quota": 0
          }
        ]
      }
    ]
  }
}
```

For resources that are measured rather than counted, the values are interpreted with the same unit that is mentioned for
this resource in `GET /domains/:domain_id`. All resources that are not mentioned in the request body remain unchanged.
This operation will not affect any project quotas in this domain.

Returns 200 (OK) on success, with a response body identical to `GET` on the same URL, containing the updated quota
values.

## PUT /domains/:domain\_id/projects/:project\_id

Set quotas for the given project. Requires a domain-admin token for the specified domain. Other than that, the call
works in the same way as `PUT /domains/:domain_id`.