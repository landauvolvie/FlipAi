# SBOM release behavior

FlipAi generates the CycloneDX SBOM inside the main release workflow before publication. The installer, SHA256 manifest, and SBOM are attested and uploaded together so a release does not depend on a second release-event workflow firing after GitHub Actions publishes it.
