select id, section_id, elem_type_id from skai.elements 
where id = $1::integer
and section_id = $2::integer;