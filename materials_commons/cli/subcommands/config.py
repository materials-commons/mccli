import os
import json
import argparse

import materials_commons.api as mcapi
from .. import functions as clifuncs
from ..user_config import Config

def config_subcommand(argv):
    """
    Configure `mc`

    mc config [--set-globus-endpoint-id <ID>]

    """
    parser = argparse.ArgumentParser(
        description='Configure `mc`',
        prog='mc config')
    parser.add_argument('--set-globus-endpoint-id', type=str, help='Set local globus endpoint ID')
    parser.add_argument('--clear-globus-endpoint-id', action="store_true", default=False, help='Clear local globus endpoint ID')

    # ignore 'mc init'
    args = parser.parse_args(argv)

    if args.set_globus_endpoint_id:
        config = Config()
        config.globus.endpoint_id = args.set_globus_endpoint_id
        config.save()

    elif args.clear_globus_endpoint_id:
        config = Config()
        config.globus.endpoint_id = None
        config.save()

    else:
        config = Config()
        print("Globus endpoint id:", config.globus.endpoint_id)
