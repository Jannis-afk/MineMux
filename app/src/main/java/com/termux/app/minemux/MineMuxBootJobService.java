package com.termux.app.minemux;

import android.app.job.JobParameters;
import android.app.job.JobService;
import android.os.PersistableBundle;
import android.util.Log;

public class MineMuxBootJobService extends JobService {

    public static final String SCRIPT_FILE_PATH = "com.termux.minemux.boot.script_path";
    private static final String TAG = "MineMuxBoot";

    @Override
    public boolean onStartJob(JobParameters params) {
        PersistableBundle extras = params.getExtras();
        String filePath = extras.getString(SCRIPT_FILE_PATH);
        if (filePath != null) {
            Log.i(TAG, "Executing boot script " + filePath);
            MineMuxRuntime.runBootScript(getApplicationContext(), filePath);
        }
        return false;
    }

    @Override
    public boolean onStopJob(JobParameters params) {
        return false;
    }
}
