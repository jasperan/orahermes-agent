"""OCI GenAI client wrapper using oci-openai."""

import os

from oci_openai import OciOpenAI, AsyncOciOpenAI, OciUserPrincipalAuth


OCI_GENAI_URL_TEMPLATE = (
    "https://inference.generativeai.{region}.oci.oraclecloud.com/20231130/actions/v1"
)

DEFAULT_REGION = "us-chicago-1"
DEFAULT_PROFILE = "foosball"
DEFAULT_COMPARTMENT_ID = os.getenv(
    "OCI_COMPARTMENT_ID",
    "ocid1.compartment.oc1..aaaaaaaaksv5b2aasfqfrmco2r2wh33vxldqhbsok67w5ldk6thkx4hn3mxa",
)


def get_oci_base_url(region: str = DEFAULT_REGION) -> str:
    """Return the OCI GenAI OpenAI-compatible base URL for a region."""
    return OCI_GENAI_URL_TEMPLATE.format(region=region)


def create_oci_client(
    profile_name: str = DEFAULT_PROFILE,
    compartment_id: str = DEFAULT_COMPARTMENT_ID,
    region: str = DEFAULT_REGION,
    **kwargs,
) -> OciOpenAI:
    """Create a synchronous OCI GenAI client."""
    return OciOpenAI(
        base_url=get_oci_base_url(region),
        auth=OciUserPrincipalAuth(profile_name=profile_name),
        compartment_id=compartment_id,
        **kwargs,
    )


def create_oci_async_client(
    profile_name: str = DEFAULT_PROFILE,
    compartment_id: str = DEFAULT_COMPARTMENT_ID,
    region: str = DEFAULT_REGION,
    **kwargs,
) -> AsyncOciOpenAI:
    """Create an asynchronous OCI GenAI client."""
    return AsyncOciOpenAI(
        base_url=get_oci_base_url(region),
        auth=OciUserPrincipalAuth(profile_name=profile_name),
        compartment_id=compartment_id,
        **kwargs,
    )
