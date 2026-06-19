select id, section_id, elem_type_id from skai.elements
where id = $1
or id = $2;