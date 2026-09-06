# Front Door enrollment upgrade

Customer control planes must never receive the vendor administration secret.
The revised customer route is `PUT /enroll/v1/tenants/{id}`. RackOps Admin no
longer mounts the global `/admin/*` endpoints on its public console listener.
The standalone edge's `/admin/*` API remains an operator-only private API.

## Register and connect

1. Configure the customer POP, zone and Admin URL. Turn Front Door on to
   create the install's tenant ID and allow the edge agent to report its
   public key. Enrollment remains pending until an operator registers it.
2. Retrieve `tenantId`, `workspace`, and `nodeId` through the authenticated
   `GET /api/frontdoor`; retrieve that node's `frontDoorKey` through
   `GET /api/nodes/{id}`. These are identifiers/public keys, never private keys.
3. In RackOps Admin's Front Door page, register that tenant ID, workspace and
   public key. The console returns a tenant-bound enrollment credential.
   For an existing tenant use its **Issue credential** button.
4. Set the customer's `--frontdoor-token` to this credential and restart its
   control plane. Its next registration updates only its own hostnames.

The authenticated console API is `POST /api/frontdoor/tenants/{id}/enrollment`.
It returns `token` and `expiresAt`, with `Cache-Control: no-store`.
The standalone operator API offers `POST /admin/tenants/{id}/enrollment`.

## Expiry and recovery

Grants expire after 24 hours. Expiry does not stop already established app
routing, but subsequent registration/hostname changes need a new grant.
Rotate the registered public key or workspace to invalidate older grants for
that identity immediately. Tenant suspension also blocks enrollment.
Rotating the vendor signing secret invalidates all outstanding grants.
Keep the vendor secret private and back it up securely.

During upgrade, issue replacement tenant credentials and remove distributed
vendor tokens. Never fall back to accepting vendor tokens on enrollment
routes. If the old secret was distributed, rotate it on the vendor side.
The manual registration/renewal step is an interim operator workflow;
self-service proof of identity and automated renewal remain future work.
