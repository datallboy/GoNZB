UPDATE settings_module_options
SET options_json = json_remove(options_json, '$.provider_group_inventory')
WHERE module_name = 'usenet_indexer'
  AND json_valid(options_json);
