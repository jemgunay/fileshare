$(document).ready(function() {
    // init dropzone
    Dropzone.options.fileInput = {
        paramName: "file-input", // The name that will be used to transfer the file
        maxFilesize: 10, // MB

        init: function() {
            this.on("success", function(file, response) {
                $("#upload-results-panel").append(response);

                initUploadForm();
            });
        }
    };

    initUploadForm();
});


function initUploadForm() {
    // reset existing events


    // set up autocomplete fields
    performRequest(hostname + "/data?fetch=tags,people", "GET", "", function (result) {
        var tokenfieldSets = [["tags", "#tags-input", false], ["people", "#people-input", false]];
        var parsedData = JSON.parse(result);

        initMetaDataFields(parsedData, tokenfieldSets);
    });

    // set initial states
    setButtonProcessing($(".btn-primary, .btn-danger"), false);

    // for each panel, destroy old events and set up new events
    $(".upload-result-panel").each(function() {
        var panel = $(this);
        var imgName = panel.find(".img-details input[type=hidden]").val();

        // perform publish file request
        panel.find("form .btn-primary").on("click", function(e) {
            e.preventDefault();
            setButtonProcessing($(this), true);

            // perform request
            performRequest(hostname + "/upload/store", "POST", $(".upload-result-container form").serialize(),
            // success
            function (result) {
                setAlertWindow("success", "File '" + imgName + "' successfully published!", "#error-window");
            },
            // error
            function (result) {
                if (result === "no_tags") {
                    setAlertWindow("warning", "Please specify at least one tag for '" + imgName + "'.", "#error-window");
                }
                else if (result === "no_people") {
                    setAlertWindow("warning", "Please specify at least one person for '" + imgName + "'.", "#error-window");
                }
                else {
                    setAlertWindow("danger", "A server error occurred (" + imgName + ").", "#error-window");
                }
            });
        });

        // delete image from user's temp dir
        panel.find("form .btn-danger").on("click", function(e) {
            e.preventDefault();
            setButtonProcessing($(this), true);

            // perform request
            performRequest(hostname + "/upload/temp_delete", "POST", $(".upload-result-container form").serialize(),
            // success
            function () {
                panel.fadeOut(0.8, function() {
                    panel.remove();
                });
                setAlertWindow("success", "File '" + imgName + "' deleted!", "#error-window");
            },
            // error
            function (result) {
                panel.fadeOut(0.8, function() {
                    panel.remove();
                });
                if (result === "invalid_file") {
                    setAlertWindow("success", "File '" + imgName + "' has already been deleted!", "#error-window");
                }
                else {
                    setAlertWindow("danger", "A server error occurred (" + imgName + ").", "#error-window");
                }
            });
        });
    });
}