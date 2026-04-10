#!/usr/bin/env python3
import argparse
import json
import os
import sys

try:
    import requests
    import yaml
except ImportError as e:
    print("Missing dependency:", e, file=sys.stderr)
    print("Install with: pip install requests pyyaml", file=sys.stderr)
    sys.exit(1)

ENDPOINTS = {
    "sites": "dcim/sites",
    "manufacturers": "dcim/manufacturers",
    "device-roles": "dcim/device-roles",
    "device-types": "dcim/device-types",
    "devices": "dcim/devices",
    "interfaces": "dcim/interfaces",
    # "cables": "dcim/cables",
    "ip-addresses": "ipam/ip-addresses",
}


def netbox_get(base_url, headers, path, params=None):
    url = f"{base_url.rstrip('/')}/api/{path}/"
    r = requests.get(url, headers=headers, params=params, timeout=60)
    r.raise_for_status()
    return r.json()


def netbox_find_id(base_url, headers, path, field, value):
    if value is None:
        return None
    params = {field: value}
    try:
        r = netbox_get(base_url, headers, path, params)
    except Exception:
        return None
    results = r.get("results", [])
    if results:
        return results[0].get("id")
    return None


def resolve_relation(base_url, headers, path, value):
    if value is None:
        return None
    if isinstance(value, int):
        return value
    if isinstance(value, dict):
        if "id" in value:
            return value["id"]
        if "pk" in value:
            return value["pk"]
        if "name" in value:
            return netbox_find_id(base_url, headers, path, "name", value["name"])
        if "slug" in value:
            return netbox_find_id(base_url, headers, path, "slug", value["slug"])
        return None

    if isinstance(value, str):
        if value.isdigit():
            return int(value)
        found = netbox_find_id(base_url, headers, path, "name", value)
        if found:
            return found
        return netbox_find_id(base_url, headers, path, "slug", value)
    return None


def get_interface_id(base_url, headers, device_ref, iface_name):
    if device_ref is None or iface_name is None:
        return None
    device_id = None
    if isinstance(device_ref, int):
        device_id = device_ref
    else:
        device_id = resolve_relation(base_url, headers, "dcim/devices", device_ref)
    if not device_id:
        return None
    params = {"device_id": device_id, "name": iface_name}
    r = netbox_get(base_url, headers, "dcim/interfaces", params)
    results = r.get("results", [])
    if results:
        return results[0].get("id")
    return None


def update_device_primary_ip(base_url, headers, interface_id, ip_id):
    r = netbox_get(base_url, headers, f"dcim/interfaces/{interface_id}")
    device = r.get("device")
    if not device:
        return
    device_id = device.get("id")
    if not device_id:
        return
    url = f"{base_url.rstrip('/')}/api/dcim/devices/{device_id}/"
    data = {"primary_ip4": ip_id}
    r2 = requests.patch(url, headers=headers, json=data, timeout=60)
    if r2.status_code not in (200, 202):
        print(f"Warning: failed to update primary_ip4 for device {device_id}: {r2.status_code} {r2.text}")


def resolve_ip_address(base_url, headers, address):
    r = netbox_get(base_url, headers, "ipam/ip-addresses", {"address": address})
    results = r.get("results", [])
    if results:
        return results[0].get("id")
    return None


def ensure_ip_assigned_to_interface(base_url, headers, ip_id, interface_id):
    url = f"{base_url.rstrip('/')}/api/ipam/ip-addresses/{ip_id}/"
    r = requests.get(url, headers=headers, timeout=60)
    if r.status_code not in (200, 201):
        print(f"Warning: failed to get IP address {ip_id}: {r.status_code} {r.text}")
        return False

    assigned = r.json().get("assigned_object")
    if assigned and assigned.get("object_type") == "dcim.interface" and assigned.get("id") == interface_id:
        return True

    r2 = requests.patch(url, headers=headers, json={"assigned_object_type": "dcim.interface", "assigned_object_id": interface_id}, timeout=60)
    if r2.status_code not in (200, 202):
        print(f"Warning: failed to assign IP {ip_id} to interface {interface_id}: {r2.status_code} {r2.text}")
        return False
    return True


def post_items(base_url, token, dirname, force=False):
    if not token:
        raise ValueError("NETBOX_TOKEN must be provided")

    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json",
        "Accept": "application/json",
    }

    primary_ip_entries = []

    cache = {
        'sites': {},
        'manufacturers': {},
        'device-roles': {},
        'device-types': {},
        'devices': {},
    }

    def resolve_cached_id(path_key, value):
        path_mapping = {
            'dcim/sites': 'sites',
            'dcim/manufacturers': 'manufacturers',
            'dcim/device-roles': 'device-roles',
            'dcim/device-types': 'device-types',
            'dcim/devices': 'devices',
            'sites': 'sites',
            'manufacturers': 'manufacturers',
            'device-roles': 'device-roles',
            'device-types': 'device-types',
            'devices': 'devices',
        }
        cache_key = path_mapping.get(path_key, None)

        if isinstance(value, dict):
            if 'id' in value:
                return value['id']
            if 'name' in value and cache_key and value['name'] in cache.get(cache_key, {}):
                return cache[cache_key][value['name']]
            if 'slug' in value and cache_key and value['slug'] in cache.get(cache_key, {}):
                return cache[cache_key][value['slug']]
            if 'name' in value:
                return resolve_relation(base_url, headers, path_key, value['name'])
            if 'slug' in value:
                return resolve_relation(base_url, headers, path_key, value['slug'])
            return None

        if isinstance(value, str):
            if cache_key and value in cache.get(cache_key, {}):
                return cache[cache_key][value]
            return resolve_relation(base_url, headers, path_key, value)

        if isinstance(value, int):
            return value

        return None

    for name, path in ENDPOINTS.items():
        file_path = os.path.join(dirname, f"{name}.yaml")
        if not os.path.isfile(file_path):
            print(f"Skipping {name}: file not found: {file_path}")
            continue

        with open(file_path, "r", encoding="utf-8") as f:
            payload = yaml.safe_load(f)

        if not payload:
            print(f"Skipping {name}: empty payload in {file_path}")
            continue

        if isinstance(payload, list):
            items = payload
        elif isinstance(payload, dict):
            if name in payload and isinstance(payload[name], list):
                items = payload[name]
            elif len(payload) == 1 and isinstance(next(iter(payload.values())), list):
                items = next(iter(payload.values()))
            else:
                items = [payload]
        else:
            print(f"Skipping {name}: invalid payload type {type(payload)} in {file_path}")
            continue

        if not items:
            print(f"Skipping {name}: no entries")
            continue

        print(f"Posting {len(items)} {name} entries to {path}...")
        for i, obj in enumerate(items, 1):
            data = obj.copy() if isinstance(obj, dict) else obj

            if path == "dcim/device-types":
                manufacturer = data.get("manufacturer")
                if manufacturer:
                    mid = resolve_relation(base_url, headers, "dcim/manufacturers", manufacturer)
                    if mid:
                        data["manufacturer"] = mid

            if path == "dcim/devices":
                data.pop("manufacturer", None)

                for field, endpoint in (
                    ("role", "dcim/device-roles"),
                    ("device_type", "dcim/device-types"),
                    ("site", "dcim/sites"),
                ):
                    if field in data:
                        resolved = resolve_relation(base_url, headers, endpoint, data[field])
                        if resolved:
                            data[field] = resolved

            if path == "dcim/interfaces":
                device = data.get("device")
                if device is not None:
                    did = resolve_cached_id("dcim/devices", device)
                    if did:
                        data["device"] = did
                        if force:
                            existing_id = get_interface_id(base_url, headers, did, data.get("name"))
                            if existing_id:
                                del_url = f"{base_url.rstrip('/')}/api/dcim/interfaces/{existing_id}/"
                                requests.delete(del_url, headers=headers, timeout=60)
                                print(f"  {name}[{i}] deleted existing interface")
                    else:
                        print(f"  {name}[{i}] warning: cannot resolve device {device}")

            primary_ip4 = False
            if path == "ipam/ip-addresses":
                assigned_object = data.pop("assigned_object", None)
                if assigned_object and isinstance(assigned_object, dict):
                    assigned_device = assigned_object.get("device")
                    assigned_name = assigned_object.get("name")
                    device_id = resolve_cached_id("dcim/devices", assigned_device)
                    if assigned_device and assigned_name:
                        interface_id = get_interface_id(base_url, headers, device_id if device_id else assigned_device, assigned_name)
                        if interface_id:
                            data["assigned_object_type"] = "dcim.interface"
                            data["assigned_object_id"] = interface_id
                            # preserve mapping for later primary update
                            if "primary" in obj and bool(obj.get("primary")):
                                primary_ip_entries.append({
                                    "address": data.get("address"),
                                    "device_id": device_id,
                                    "interface": assigned_name,
                                })
                if "primary" in data:
                    primary_ip4 = bool(data.pop("primary"))

            url = f"{base_url.rstrip('/')}/api/{path}/"
            r = requests.post(url, headers=headers, json=data, timeout=60)

            if r.status_code in (200, 201):
                resp = r.json()
                print(f"  {name}[{i}] created")
                if name == 'sites':
                    cache['sites'][resp.get('name')] = resp.get('id')
                    cache['sites'][resp.get('slug')] = resp.get('id')
                elif name == 'manufacturers':
                    cache['manufacturers'][resp.get('name')] = resp.get('id')
                    cache['manufacturers'][resp.get('slug')] = resp.get('id')
                elif name == 'device-roles':
                    cache['device-roles'][resp.get('name')] = resp.get('id')
                    cache['device-roles'][resp.get('slug')] = resp.get('id')
                elif name == 'device-types':
                    cache['device-types'][resp.get('slug')] = resp.get('id')
                elif name == 'devices':
                    cache['devices'][resp.get('name')] = resp.get('id')

            elif r.status_code in (409, 400):
                reason = "already exists" if r.status_code == 409 else f"invalid ({r.text})"
                print(f"  {name}[{i}] skipped ({reason})")
                if name in ('sites', 'manufacturers', 'device-roles', 'device-types', 'devices'):
                    key = None
                    if isinstance(obj, dict):
                        key = obj.get('name') or obj.get('slug')
                    if not key and isinstance(obj, str):
                        key = obj
                    if key:
                        existing_id = resolve_relation(base_url, headers, path, key)
                        if existing_id:
                            cache_key = name
                            cache[cache_key][key] = existing_id

            else:
                print(f"  {name}[{i}] failed: {r.status_code} {r.text}")
                r.raise_for_status()

    # apply primary IP updates based on YAML intent (and tolerate existing/duplicated records)
    for info in primary_ip_entries:
        ip_id = resolve_ip_address(base_url, headers, info["address"])
        interface_ref = info.get("device_id") if info.get("device_id") is not None else info.get("device")
        interface_id = get_interface_id(base_url, headers, interface_ref, info["interface"])
        if not ip_id or not interface_id:
            print(f"Skipping primary assignment for {info}: missing IP/interface")
            continue
        if ensure_ip_assigned_to_interface(base_url, headers, ip_id, interface_id):
            update_device_primary_ip(base_url, headers, interface_id, ip_id)


if __name__ == "__main__":
    p = argparse.ArgumentParser(description="Publish NetBox initializers via REST API")
    p.add_argument("netbox_url", help="Base URL for NetBox (http://host:80)")
    p.add_argument("token", help="NetBox API token")
    p.add_argument("init_dir", help="directory with initializers *.yaml")
    p.add_argument("--force", action="store_true", help="Force recreate interfaces and IPs even if they exist")
    args = p.parse_args()

    post_items(args.netbox_url, args.token, args.init_dir, args.force)
