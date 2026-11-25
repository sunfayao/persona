package assistant

import ()

func northStarWeeklyFindRow(rows []northStarRowOut, id string) *northStarRowOut {
	for i := range rows {
		if rows[i].ID == id {
			return &rows[i]
		}
	}
	return nil
}
