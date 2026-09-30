"""Naming shared by the fixtures that wire resources up and the tests that assert on them."""

API_VERSION = "resources.signoz.io/v1alpha1"

# Every resource carries a short interval so a drift or edit is picked up
# within a test's patience rather than at the operator-wide ten-minute default.
RESOURCE_INTERVAL = "5s"

# Pins the SigNoz object a resource adopts, and keeps the custom resource alive
# until its reclaim policy has been applied.
ANNOTATION_SIGNOZ_RESOURCE_ID = "resources.signoz.io/signoz-resource-id"
RESOURCE_FINALIZER = "resources.signoz.io/finalizer"
